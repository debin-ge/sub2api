package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/wenlng/go-captcha/v2/click"
	"github.com/wenlng/go-captcha/v2/rotate"
	"github.com/wenlng/go-captcha/v2/slide"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// 这套测试跨越 routes -> handler -> service -> repository 四层，用真实的图像生成
// 与真实的 Redis（miniredis）跑完整的「申请挑战 -> 作答 -> 换取令牌」闭环。
// 放在 routes 包而不是 handler 包，是因为 handler 被 depguard 禁止依赖 repository。

// captchaSettingRepoStub 是只读的内存设置仓库，只为让 SettingService 能读到
// gocaptcha_enabled / gocaptcha_mode。
type captchaSettingRepoStub struct {
	values map[string]string
}

func (s *captchaSettingRepoStub) Get(_ context.Context, key string) (*service.Setting, error) {
	return &service.Setting{Key: key, Value: s.values[key]}, nil
}

func (s *captchaSettingRepoStub) GetValue(_ context.Context, key string) (string, error) {
	return s.values[key], nil
}

func (s *captchaSettingRepoStub) Set(_ context.Context, key, value string) error {
	s.values[key] = value
	return nil
}

func (s *captchaSettingRepoStub) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	result := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := s.values[key]; ok {
			result[key] = value
		}
	}
	return result, nil
}

func (s *captchaSettingRepoStub) SetMultiple(_ context.Context, settings map[string]string) error {
	for key, value := range settings {
		s.values[key] = value
	}
	return nil
}

func (s *captchaSettingRepoStub) GetAll(_ context.Context) (map[string]string, error) {
	return s.values, nil
}

func (s *captchaSettingRepoStub) Delete(_ context.Context, key string) error {
	delete(s.values, key)
	return nil
}

type captchaTestEnv struct {
	router *gin.Engine
	rdb    *redis.Client
}

func newCaptchaTestEnv(t *testing.T, mode string) *captchaTestEnv {
	t.Helper()
	return newCaptchaTestEnvWithSettings(t, map[string]string{
		service.SettingKeyGoCaptchaEnabled: "true",
		service.SettingKeyGoCaptchaMode:    mode,
	})
}

func newCaptchaTestEnvWithSettings(t *testing.T, settings map[string]string) *captchaTestEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { require.NoError(t, rdb.Close()) })

	cfg := &config.Config{
		GoCaptcha: config.GoCaptchaConfig{
			ChallengeTTL:        time.Minute,
			TokenTTL:            5 * time.Minute,
			PaddingClick:        5,
			PaddingSlide:        5,
			PaddingRotate:       8,
			ClickVerifyLen:      3,
			ChallengeRatePerMin: 1000,
			VerifyRatePerMin:    1000,
			MaxFailures:         3,
			FailureWindow:       10 * time.Minute,
			Cooldown:            10 * time.Minute,
		},
	}
	settingService := service.NewSettingService(&captchaSettingRepoStub{values: settings}, cfg)
	goCaptchaService := service.NewGoCaptchaService(
		settingService,
		repository.NewGoCaptchaGenerator(cfg),
		repository.NewGoCaptchaCache(rdb),
		cfg,
	)

	router := gin.New()
	v1 := router.Group("/api/v1")
	RegisterCaptchaRoutes(v1, &handler.Handlers{
		Captcha: handler.NewCaptchaHandler(goCaptchaService),
	}, rdb, cfg)

	return &captchaTestEnv{router: router, rdb: rdb}
}

func (e *captchaTestEnv) post(t *testing.T, path string, body any) (int, map[string]any) {
	t.Helper()

	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		require.NoError(t, err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &parsed))
	return rec.Code, parsed
}

// solveFromRedis 扮演一个知道答案的求解器：直接从 Redis 读出服务端保存的答案，
// 拼成用户本该提交的 answer 字符串，从而覆盖真实的图像生成流程。
func (e *captchaTestEnv) solveFromRedis(t *testing.T, captchaID, mode string) string {
	t.Helper()

	raw, err := e.rdb.Get(context.Background(), "gocaptcha:challenge:"+captchaID).Bytes()
	require.NoError(t, err)

	var stored struct {
		Mode   string          `json:"mode"`
		Answer json.RawMessage `json:"answer"`
	}
	require.NoError(t, json.Unmarshal(raw, &stored))
	require.Equal(t, mode, stored.Mode)

	switch mode {
	case "slide", "drag":
		var block slide.Block
		require.NoError(t, json.Unmarshal(stored.Answer, &block))
		return fmt.Sprintf("%d,%d", block.X, block.Y)
	case "rotate":
		var block rotate.Block
		require.NoError(t, json.Unmarshal(stored.Answer, &block))
		return fmt.Sprintf("%d", 360-block.Angle)
	default:
		var dots map[int]*click.Dot
		require.NoError(t, json.Unmarshal(stored.Answer, &dots))
		parts := make([]string, 0, len(dots)*2)
		for i := 0; i < len(dots); i++ {
			// Use the center of the generated target rather than a boundary point.
			// Rotated glyph bounds can land exactly on a rounding edge; a real user
			// also naturally clicks near the center of the visible target.
			parts = append(parts, fmt.Sprintf(
				"%d,%d",
				dots[i].X+dots[i].Width/2,
				dots[i].Y+dots[i].Height/2,
			))
		}
		return joinComma(parts)
	}
}

func joinComma(parts []string) string {
	out := ""
	for i, part := range parts {
		if i > 0 {
			out += ","
		}
		out += part
	}
	return out
}

func captchaData(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	data, ok := body["data"].(map[string]any)
	require.True(t, ok, "expected a data object, got %v", body)
	return data
}

var goCaptchaModes = []string{"click", "shape", "slide", "drag", "rotate"}

func TestCaptchaEndToEndForEveryMode(t *testing.T) {
	for _, mode := range goCaptchaModes {
		t.Run(mode, func(t *testing.T) {
			env := newCaptchaTestEnv(t, mode)

			status, body := env.post(t, "/api/v1/captcha/challenge", nil)
			require.Equal(t, http.StatusOK, status)
			challenge := captchaData(t, body)
			captchaID, _ := challenge["captcha_id"].(string)
			require.NotEmpty(t, captchaID)
			require.Equal(t, mode, challenge["mode"])
			require.NotEmpty(t, challenge["master_image"])
			require.NotEmpty(t, challenge["thumb_image"])

			answer := env.solveFromRedis(t, captchaID, mode)
			status, body = env.post(t, "/api/v1/captcha/verify", map[string]string{
				"captcha_id": captchaID,
				"answer":     answer,
			})
			require.Equal(t, http.StatusOK, status, "verify failed: %v", body)
			token, _ := captchaData(t, body)["token"].(string)
			require.NotEmpty(t, token)
		})
	}
}

func TestCaptchaChallengeResponseNeverCarriesTheAnswer(t *testing.T) {
	// 白名单校验：任何新增到挑战响应里的字段都必须显式评估是否泄露答案
	allowed := map[string]bool{
		"captcha_id": true, "mode": true, "master_image": true, "thumb_image": true,
		"tile_x": true, "tile_y": true, "tile_width": true, "tile_height": true,
		"thumb_size": true,
	}

	for _, mode := range goCaptchaModes {
		t.Run(mode, func(t *testing.T) {
			env := newCaptchaTestEnv(t, mode)

			status, body := env.post(t, "/api/v1/captcha/challenge", nil)
			require.Equal(t, http.StatusOK, status)

			for key := range captchaData(t, body) {
				require.True(t, allowed[key], "unexpected field %q in challenge response", key)
			}
		})
	}
}

func TestCaptchaChallengeIsSingleUseOverHTTP(t *testing.T) {
	env := newCaptchaTestEnv(t, "click")

	_, body := env.post(t, "/api/v1/captcha/challenge", nil)
	captchaID, _ := captchaData(t, body)["captcha_id"].(string)
	answer := env.solveFromRedis(t, captchaID, "click")

	status, verifyBody := env.post(t, "/api/v1/captcha/verify", map[string]string{
		"captcha_id": captchaID, "answer": answer,
	})
	require.Equal(t, http.StatusOK, status, "correct generated answer should verify: answer=%s body=%v", answer, verifyBody)

	// 同一个挑战重放必须失败
	status, _ = env.post(t, "/api/v1/captcha/verify", map[string]string{
		"captcha_id": captchaID, "answer": answer,
	})
	require.Equal(t, http.StatusBadRequest, status)
}

func TestCaptchaWrongAnswerAndUnknownIDAreIndistinguishable(t *testing.T) {
	env := newCaptchaTestEnv(t, "click")

	_, body := env.post(t, "/api/v1/captcha/challenge", nil)
	captchaID, _ := captchaData(t, body)["captcha_id"].(string)

	wrongStatus, wrongBody := env.post(t, "/api/v1/captcha/verify", map[string]string{
		"captcha_id": captchaID,
		"answer":     "1,1,2,2,3,3",
	})
	unknownStatus, unknownBody := env.post(t, "/api/v1/captcha/verify", map[string]string{
		"captcha_id": "does-not-exist",
		"answer":     "1,1,2,2,3,3",
	})

	require.Equal(t, http.StatusBadRequest, wrongStatus)
	require.Equal(t, unknownStatus, wrongStatus)
	require.Equal(t, unknownBody["message"], wrongBody["message"])
	require.Equal(t, unknownBody["reason"], wrongBody["reason"])
}

func TestCaptchaMalformedRequestLooksLikeAWrongAnswer(t *testing.T) {
	env := newCaptchaTestEnv(t, "click")

	status, body := env.post(t, "/api/v1/captcha/verify", map[string]string{"captcha_id": ""})

	require.Equal(t, http.StatusBadRequest, status)
	require.Equal(t, "GO_CAPTCHA_VERIFICATION_FAILED", body["reason"])
}

func TestCaptchaVerifyRejectsOversizedBody(t *testing.T) {
	env := newCaptchaTestEnv(t, "click")

	status, body := env.post(t, "/api/v1/captcha/verify", map[string]string{
		"captcha_id": "id",
		"answer":     strings.Repeat("1", 9*1024),
	})

	require.Equal(t, http.StatusBadRequest, status)
	require.Equal(t, "GO_CAPTCHA_VERIFICATION_FAILED", body["reason"])
}

func TestCaptchaConsecutiveFailuresTriggerCooldown(t *testing.T) {
	env := newCaptchaTestEnv(t, "slide")

	// MaxFailures 为 3：滑动模式盲猜成本低，冷却是它的主要防爆破手段
	for i := 0; i < 3; i++ {
		_, body := env.post(t, "/api/v1/captcha/challenge", nil)
		captchaID, _ := captchaData(t, body)["captcha_id"].(string)
		require.NotEmpty(t, captchaID)

		status, _ := env.post(t, "/api/v1/captcha/verify", map[string]string{
			"captcha_id": captchaID, "answer": "0,0",
		})
		require.Equal(t, http.StatusBadRequest, status)
	}

	status, body := env.post(t, "/api/v1/captcha/challenge", nil)
	require.Equal(t, http.StatusTooManyRequests, status)
	require.Equal(t, "GO_CAPTCHA_TOO_MANY_FAILURES", body["reason"])
}

func TestCaptchaChallengeRejectedWhenProviderDisabled(t *testing.T) {
	env := newCaptchaTestEnvWithSettings(t, map[string]string{})

	status, body := env.post(t, "/api/v1/captcha/challenge", nil)

	require.Equal(t, http.StatusServiceUnavailable, status)
	require.Equal(t, "GO_CAPTCHA_NOT_ENABLED", body["reason"])
}

func TestCaptchaChallengeRateLimited(t *testing.T) {
	env := newCaptchaTestEnvWithSettings(t, map[string]string{
		service.SettingKeyGoCaptchaEnabled: "true",
		service.SettingKeyGoCaptchaMode:    "click",
	})
	// 挑战生成是 CPU 密集的图像合成，且接口免鉴权，限流必须真实生效
	env.router = newRateLimitedCaptchaRouter(t, env.rdb)

	var limited bool
	for i := 0; i < 4; i++ {
		status, _ := env.post(t, "/api/v1/captcha/challenge", nil)
		if status == http.StatusTooManyRequests {
			limited = true
			break
		}
	}
	require.True(t, limited, "challenge endpoint must be rate limited")
}

func newRateLimitedCaptchaRouter(t *testing.T, rdb *redis.Client) *gin.Engine {
	t.Helper()

	cfg := &config.Config{
		GoCaptcha: config.GoCaptchaConfig{
			ChallengeTTL:        time.Minute,
			TokenTTL:            time.Minute,
			ClickVerifyLen:      3,
			ChallengeRatePerMin: 2,
			VerifyRatePerMin:    2,
		},
	}
	settingService := service.NewSettingService(&captchaSettingRepoStub{values: map[string]string{
		service.SettingKeyGoCaptchaEnabled: "true",
		service.SettingKeyGoCaptchaMode:    "click",
	}}, cfg)
	goCaptchaService := service.NewGoCaptchaService(
		settingService,
		repository.NewGoCaptchaGenerator(cfg),
		repository.NewGoCaptchaCache(rdb),
		cfg,
	)

	router := gin.New()
	v1 := router.Group("/api/v1")
	RegisterCaptchaRoutes(v1, &handler.Handlers{
		Captcha: handler.NewCaptchaHandler(goCaptchaService),
	}, rdb, cfg)
	return router
}
