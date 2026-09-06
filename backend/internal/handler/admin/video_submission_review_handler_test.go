package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type submissionReviewHandlerStub struct {
	videoAdminHandlerStub
	called bool
}

func (s *submissionReviewHandlerStub) ResolveNotCreated(context.Context, string) (*service.VideoTask, error) {
	s.called = true
	return s.task, nil
}
func (s *submissionReviewHandlerStub) DecideSubmissionReview(context.Context, string, int64, bool) (*service.VideoTask, error) {
	s.called = true
	return s.task, nil
}

func TestVideoSubmissionHandlersRequireActorVersionKeyAndEvidence(t *testing.T) {
	for _, action := range []string{"created", "not_created", "approve", "reject"} {
		for _, missing := range []string{"actor", "version", "key", "reason", "evidence", "none"} {
			if (action == "approve" || action == "reject") && missing == "evidence" {
				continue
			}
			t.Run(action+"/"+missing, func(t *testing.T) {
				stub := &submissionReviewHandlerStub{videoAdminHandlerStub: videoAdminHandlerStub{task: &service.VideoTask{PublicID: service.NewVideoTaskID()}}}
				handler := newVideoHandler(stub)
				invoke := handler.ResolveCreated
				payload := map[string]any{"reason": "Verified original submission", "evidence_ref": "ticket:UNKNOWN"}
				switch action {
				case "created":
					payload["provider_task_id"] = "video_exact"
				case "not_created":
					invoke = handler.ResolveNotCreated
				case "approve":
					invoke = handler.ApproveSubmissionReview
					delete(payload, "evidence_ref")
				case "reject":
					invoke = handler.RejectSubmissionReview
					delete(payload, "evidence_ref")
				}
				if missing == "reason" {
					delete(payload, "reason")
				}
				if missing == "evidence" {
					delete(payload, "evidence_ref")
				}
				body, err := json.Marshal(payload)
				require.NoError(t, err)
				router := gin.New()
				router.Use(func(c *gin.Context) {
					if missing != "actor" {
						c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 99})
					}
					c.Next()
				})
				router.POST("/tasks/:id/reviews/:review_id", invoke)
				request := httptest.NewRequest(http.MethodPost, "/tasks/"+stub.task.PublicID+"/reviews/22", bytes.NewReader(body))
				if missing != "version" {
					request.Header.Set("If-Match", `"0"`)
				}
				if missing != "key" {
					request.Header.Set("Idempotency-Key", "submission:handler")
				}
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, request)
				if missing == "none" {
					require.Equal(t, http.StatusOK, recorder.Code)
				} else {
					require.GreaterOrEqual(t, recorder.Code, 400)
					require.False(t, stub.called)
					require.Empty(t, stub.resolvedProviderID)
				}
			})
		}
	}
}
