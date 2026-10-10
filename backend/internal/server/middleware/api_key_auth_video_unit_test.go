//go:build unit

package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestVideoResourceAuthDoesNotRequireGenerationBalance(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, keyStatus := range []string{service.StatusActive, service.StatusAPIKeyQuotaExhausted} {
		for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodDelete} {
			t.Run(keyStatus+"/"+method, func(t *testing.T) {
				group := &service.Group{ID: 7, Platform: service.PlatformOpenAI, Status: service.StatusActive, Hydrated: true}
				user := &service.User{ID: 42, Role: service.RoleUser, Status: service.StatusActive, Balance: 0}
				key := &service.APIKey{ID: 3, UserID: user.ID, Key: "video-auth-test", Status: keyStatus, User: user, GroupID: &group.ID, Group: group}
				require.Equal(t, http.StatusOK, serveVideoResourceAuthTest(t, key, method).Code)
			})
		}
	}
}

func TestVideoResourceAuthStillRejectsInvalidIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	expired := time.Now().Add(-time.Hour)
	for _, scenario := range []string{"expired_time", "expired_status", "disabled_key", "disabled_user"} {
		t.Run(scenario, func(t *testing.T) {
			group := &service.Group{ID: 7, Platform: service.PlatformOpenAI, Status: service.StatusActive, Hydrated: true}
			user := &service.User{ID: 42, Role: service.RoleUser, Status: service.StatusActive, Balance: 0}
			key := &service.APIKey{ID: 3, UserID: user.ID, Key: "video-auth-test", Status: service.StatusActive, User: user, GroupID: &group.ID, Group: group}
			switch scenario {
			case "expired_time":
				key.ExpiresAt = &expired
			case "expired_status":
				key.Status = service.StatusAPIKeyExpired
			case "disabled_key":
				key.Status = "disabled"
			case "disabled_user":
				user.Status = "disabled"
			}
			require.GreaterOrEqual(t, serveVideoResourceAuthTest(t, key, http.MethodGet).Code, 400)
		})
	}
}

func serveVideoResourceAuthTest(t *testing.T, key *service.APIKey, method string) *httptest.ResponseRecorder {
	t.Helper()
	repo := &stubApiKeyRepo{getByKey: func(context.Context, string) (*service.APIKey, error) { return key, nil }}
	cfg := &config.Config{RunMode: config.RunModeStandard}
	svc := service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, cfg)
	router := gin.New()
	router.Use(gin.HandlerFunc(NewAPIKeyAuthMiddleware(svc, nil, cfg)))
	path := "/v1/videos/video_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if method == http.MethodHead || method == http.MethodGet {
		path += "/content"
	}
	router.Handle(method, path, func(ctx *gin.Context) { ctx.Status(http.StatusOK) })
	request := httptest.NewRequest(method, path, nil)
	request.Header.Set("x-api-key", key.Key)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func TestCompositeVideoPostDefersBudgetButRetainsIdentityGates(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, scenario := range []string{"zero_balance", "quota_exhausted", "expired", "disabled"} {
		for _, path := range []string{"/videos", "/v1/videos/edits", "/videos/extensions", "/v1/videos/characters"} {
			t.Run(scenario+path, func(t *testing.T) {
				group := &service.Group{ID: 7, Platform: service.PlatformComposite, Status: service.StatusActive, Hydrated: true}
				user := &service.User{ID: 42, Role: service.RoleUser, Status: service.StatusActive, Balance: 0}
				key := &service.APIKey{ID: 3, UserID: user.ID, Key: "video-defer-test", Status: service.StatusActive, User: user, GroupID: &group.ID, Group: group}
				expected := http.StatusOK
				switch scenario {
				case "quota_exhausted":
					key.Status = service.StatusAPIKeyQuotaExhausted
				case "expired":
					at := time.Now().Add(-time.Hour)
					key.ExpiresAt = &at
					expected = http.StatusForbidden
				case "disabled":
					key.Status = "disabled"
					expected = http.StatusUnauthorized
				}
				repo := &stubApiKeyRepo{getByKey: func(context.Context, string) (*service.APIKey, error) { return key, nil }}
				cfg := &config.Config{RunMode: config.RunModeStandard}
				svc := service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, cfg)
				router := gin.New()
				router.Use(gin.HandlerFunc(NewAPIKeyAuthMiddleware(svc, nil, cfg)))
				router.POST(path, func(ctx *gin.Context) { ctx.Status(http.StatusOK) })
				request := httptest.NewRequest(http.MethodPost, path, nil)
				request.Header.Set("x-api-key", key.Key)
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, request)
				require.Equal(t, expected, recorder.Code)
			})
		}
	}
}

// Ark 任务路径走与 /v1/videos 相同的账务规则：读与删除不要求生成余额，创建延迟到意图阶段计费，
// 但身份门禁（过期 / 禁用）照常生效。
func TestArkVideoTaskPathsFollowVideoAuthRules(t *testing.T) {
	gin.SetMode(gin.TestMode)
	taskID := "video_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	for _, platform := range []string{service.PlatformByteDance, service.PlatformFlova, service.PlatformComposite} {
		for _, scenario := range []struct {
			name, keyStatus string
			expired         bool
			expected        int
		}{
			{"quota_exhausted", service.StatusAPIKeyQuotaExhausted, false, http.StatusOK},
			{"zero_balance", service.StatusActive, false, http.StatusOK},
			{"expired", service.StatusActive, true, http.StatusForbidden},
			{"disabled", "disabled", false, http.StatusUnauthorized},
		} {
			for _, request := range []struct {
				method string
				path   string
			}{
				{http.MethodPost, "/api/v3/contents/generations/tasks"},
				{http.MethodPost, "/contents/generations/tasks"},
				{http.MethodGet, "/v1/contents/generations/tasks"},
				{http.MethodGet, "/api/v3/contents/generations/tasks/" + taskID},
				{http.MethodDelete, "/v3/contents/generations/tasks/" + taskID},
				{http.MethodGet, "/api/v3/contents/generations/tasks/" + taskID + "/content"},
				{http.MethodHead, "/api/v3/contents/generations/tasks/" + taskID + "/content"},
				{http.MethodGet, "/api/v3/contents/generations/models"},
			} {
				t.Run(platform+"/"+scenario.name+"/"+request.method+request.path, func(t *testing.T) {
					group := &service.Group{ID: 7, Platform: platform, Status: service.StatusActive, Hydrated: true}
					user := &service.User{ID: 42, Role: service.RoleUser, Status: service.StatusActive, Balance: 0}
					key := &service.APIKey{ID: 3, UserID: user.ID, Key: "ark-video-auth-test", Status: scenario.keyStatus, User: user, GroupID: &group.ID, Group: group}
					if scenario.expired {
						at := time.Now().Add(-time.Hour)
						key.ExpiresAt = &at
					}
					repo := &stubApiKeyRepo{getByKey: func(context.Context, string) (*service.APIKey, error) { return key, nil }}
					cfg := &config.Config{RunMode: config.RunModeStandard}
					svc := service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, cfg)
					router := gin.New()
					router.Use(gin.HandlerFunc(NewAPIKeyAuthMiddleware(svc, nil, cfg)))
					router.Handle(request.method, request.path, func(ctx *gin.Context) { ctx.Status(http.StatusOK) })
					httpRequest := httptest.NewRequest(request.method, request.path, nil)
					httpRequest.Header.Set("x-api-key", key.Key)
					recorder := httptest.NewRecorder()
					router.ServeHTTP(recorder, httpRequest)
					require.Equal(t, scenario.expected, recorder.Code, recorder.Body.String())
				})
			}
		}
	}
}
