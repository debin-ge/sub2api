package handler

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type videoCreateIntentAPI interface {
	BeginVideoCreateIntent(context.Context, *service.APIKey, string, string, string, []byte) (*service.VideoCreateIntentSession, error)
}

type videoCreateIntentHashAPI interface {
	BeginVideoCreateIntentWithHash(context.Context, *service.APIKey, string, string, string, string) (*service.VideoCreateIntentSession, error)
}

const videoCreateResponseMaxBytes = 1 << 20

var errVideoCreateResponseTooLarge = infraerrors.InternalServer("VIDEO_CREATE_RESPONSE_TOO_LARGE", "video creation response exceeds the buffer limit; retry with the same idempotency key")

type videoCreateResponseBuffer struct {
	gin.ResponseWriter
	headers  http.Header
	body     bytes.Buffer
	status   int
	written  bool
	overflow bool
}

func (writer *videoCreateResponseBuffer) Header() http.Header { return writer.headers }
func (writer *videoCreateResponseBuffer) Status() int         { return writer.status }
func (writer *videoCreateResponseBuffer) Size() int {
	if !writer.written {
		return -1
	}
	return writer.body.Len()
}
func (writer *videoCreateResponseBuffer) Written() bool { return writer.written }
func (writer *videoCreateResponseBuffer) WriteHeader(status int) {
	if !writer.written {
		writer.status = status
	}
}
func (writer *videoCreateResponseBuffer) WriteHeaderNow() { writer.written = true }
func (writer *videoCreateResponseBuffer) Flush()          { writer.WriteHeaderNow() }
func (writer *videoCreateResponseBuffer) Write(body []byte) (int, error) {
	writer.WriteHeaderNow()
	if writer.overflow || writer.body.Len()+len(body) > videoCreateResponseMaxBytes {
		writer.overflow = true
		return 0, errVideoCreateResponseTooLarge
	}
	return writer.body.Write(body)
}
func (writer *videoCreateResponseBuffer) WriteString(body string) (int, error) {
	return writer.Write([]byte(body))
}
func (writer *videoCreateResponseBuffer) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, errors.New("video creation does not support connection hijacking")
}
func (writer *videoCreateResponseBuffer) Pusher() http.Pusher { return nil }

func (h *VideoHandler) CreateIntentMiddleware(c *gin.Context) {
	key, ok := middleware.GetAPIKeyFromContext(c)
	jsonRequest := service.IsIdempotentJSONVideoCreate(c.Request.Method, c.Request.URL.Path, c.GetHeader("Content-Type"), c.GetHeader("Idempotency-Key"))
	if !jsonRequest {
		c.Next()
		return
	}
	if !ok || key == nil || key.Group == nil || key.GroupID == nil {
		c.Next()
		return
	}
	if key.Group.Platform != service.PlatformOpenAI && key.Group.Platform != service.PlatformComposite {
		c.Next()
		return
	}
	if target, resolved := service.ResolvedTargetPlatformFromContext(c.Request.Context()); resolved && target != service.PlatformOpenAI {
		c.Next()
		return
	}
	operation, _ := service.VideoCreateIntentOperationForPath(c.Request.URL.Path)
	if err := service.ValidateVideoReleaseOperation(operation); err != nil {
		videoError(c, err)
		c.Abort()
		return
	}
	if h == nil {
		videoError(c, service.ErrBillingServiceUnavailable)
		c.Abort()
		return
	}
	api, available := h.tasks.(videoCreateIntentAPI)
	if !available {
		videoError(c, service.ErrBillingServiceUnavailable)
		c.Abort()
		return
	}
	if err := validateVideoIdempotencyKey(c.GetHeader("Idempotency-Key")); err != nil {
		videoError(c, err)
		c.Abort()
		return
	}
	if encoding := strings.ToLower(strings.TrimSpace(c.GetHeader("Content-Encoding"))); encoding != "" && encoding != "identity" {
		videoError(c, service.ErrVideoInvalidRequest)
		c.Abort()
		return
	}
	bodyLimit := videoJSONBodyMaxBytes
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, bodyLimit+1))
	if err != nil {
		videoError(c, service.ErrVideoInvalidRequest)
		c.Abort()
		return
	}
	if int64(len(body)) > bodyLimit {
		videoError(c, service.ErrVideoInputTooLarge)
		c.Abort()
		return
	}
	if err := service.ValidateVideoReleaseJSON(operation, body); err != nil {
		videoError(c, err)
		c.Abort()
		return
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	c.Request.ContentLength = int64(len(body))
	c.Request.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
	session, err := api.BeginVideoCreateIntent(c.Request.Context(), key, operation, c.GetHeader("Idempotency-Key"), c.GetHeader("Content-Type"), body)
	if err != nil || session == nil || session.Intent == nil {
		if err == nil {
			err = service.ErrBillingServiceUnavailable
		}
		if errors.Is(err, service.ErrVideoCreateInProgress) {
			c.Header("Retry-After", "3")
		}
		videoError(c, err)
		c.Abort()
		return
	}
	h.executeVideoCreateIntentSession(c, session, c.Next, func() {
		switch operation {
		case service.VideoOperationGenerate:
			h.Create(c)
		case service.VideoOperationEdit:
			h.Edit(c)
		case service.VideoOperationExtend:
			h.Extend(c)
		case service.VideoOperationCharacterCreate:
			h.CreateCharacter(c)
		default:
			videoError(c, service.ErrVideoInvalidRequest)
		}
	})
}

func (h *VideoHandler) executeVideoCreateIntentSession(c *gin.Context, session *service.VideoCreateIntentSession, next, nativeReplay func()) {
	if c == nil || c.Request == nil || session == nil || session.Intent == nil || next == nil || nativeReplay == nil {
		videoError(c, service.ErrBillingServiceUnavailable)
		if c != nil {
			c.Abort()
		}
		return
	}
	if session.Intent.ID > 0 {
		c.Header("X-Video-Create-Intent", strconv.FormatInt(session.Intent.ID, 10))
	}
	h.setNoStore(c)
	if !session.Owned {
		c.Header("X-Video-Idempotency-Replayed", "true")
		if session.Intent.State == service.VideoCreateIntentNative {
			nativeReplay()
		} else {
			videoError(c, service.ErrVideoCreateOutcomeUnknown)
		}
		c.Abort()
		return
	}
	parent := c.Request.Context()
	ctx, cancel := context.WithCancel(session.Context(parent))
	c.Request = c.Request.WithContext(ctx)
	stop, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				renewCtx, release := context.WithTimeout(ctx, 5*time.Second)
				err := session.Renew(renewCtx)
				release()
				if err != nil {
					cancel()
					return
				}
			}
		}
	}()
	original := c.Writer
	buffer := &videoCreateResponseBuffer{ResponseWriter: original, headers: original.Header().Clone(), status: http.StatusOK}
	c.Writer = buffer
	finished := false
	defer func() {
		failure := recover()
		close(stop)
		cancel()
		<-stopped
		finishCtx, release := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
		err := session.Finish(finishCtx, buffer.status, finished && failure == nil)
		release()
		c.Writer = original
		if failure != nil {
			c.Abort()
			panic(failure)
		}
		if buffer.overflow && err == nil {
			err = errVideoCreateResponseTooLarge
		}
		if err != nil {
			videoError(c, err)
			c.Abort()
			return
		}
		for header := range original.Header() {
			delete(original.Header(), header)
		}
		for header, values := range buffer.Header() {
			original.Header()[header] = append([]string(nil), values...)
		}
		original.Header().Del("Content-Length")
		h.setNoStore(c)
		original.WriteHeader(buffer.status)
		_, _ = original.Write(buffer.body.Bytes())
	}()
	next()
	finished = true
}
