//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// newAuthServiceForGoCaptchaTest 组装一个只启用自建验证码的 AuthService，
// 并把同一个 cache 返回出去，方便测试直接塞入已签发的令牌。
func newAuthServiceForGoCaptchaTest(
	t *testing.T,
	settings map[string]string,
) (*AuthService, *GoCaptchaService, *goCaptchaCacheStub) {
	t.Helper()

	cfg := &config.Config{
		Server: config.ServerConfig{Mode: "release"},
		GoCaptcha: config.GoCaptchaConfig{
			ChallengeTTL: time.Minute,
			TokenTTL:     5 * time.Minute,
			PaddingClick: 5,
			MaxFailures:  3,
		},
	}
	settingService := NewSettingService(&settingRepoStub{values: settings}, cfg)
	cache := newGoCaptchaCacheStub()
	goCaptchaService := NewGoCaptchaService(settingService, &goCaptchaGeneratorStub{valid: true}, cache, cfg)

	svc := NewAuthService(nil, &userRepoStub{}, nil, nil, cfg, settingService, nil, nil, nil, nil, nil, nil, nil)
	svc.SetGoCaptchaService(goCaptchaService)
	return svc, goCaptchaService, cache
}

// issueGoCaptchaToken 走完整的挑战与作答流程拿到一个可用令牌。
func issueGoCaptchaToken(t *testing.T, svc *GoCaptchaService, clientIP string) string {
	t.Helper()
	view, err := svc.CreateChallenge(context.Background(), clientIP)
	require.NoError(t, err)
	token, err := svc.SolveChallenge(context.Background(), view.CaptchaID, "1,2,3,4,5,6", clientIP)
	require.NoError(t, err)
	return token
}

func TestVerifyCaptchaUsesGoCaptchaWhenEnabled(t *testing.T) {
	svc, goCaptchaService, _ := newAuthServiceForGoCaptchaTest(t, goCaptchaSettings("click"))
	token := issueGoCaptchaToken(t, goCaptchaService, "203.0.113.10")

	// 自建验证码的令牌复用 turnstile_token 字段，与阿里云的 captchaVerifyParam 同一约定
	err := svc.VerifyCaptcha(context.Background(), CaptchaProof{TurnstileToken: token}, "203.0.113.10")

	require.NoError(t, err)
}

func TestVerifyCaptchaRejectsReplayedGoCaptchaToken(t *testing.T) {
	svc, goCaptchaService, _ := newAuthServiceForGoCaptchaTest(t, goCaptchaSettings("click"))
	token := issueGoCaptchaToken(t, goCaptchaService, "203.0.113.10")
	ctx := context.Background()

	require.NoError(t, svc.VerifyCaptcha(ctx, CaptchaProof{TurnstileToken: token}, "203.0.113.10"))
	require.ErrorIs(t,
		svc.VerifyCaptcha(ctx, CaptchaProof{TurnstileToken: token}, "203.0.113.10"),
		ErrGoCaptchaVerificationFailed,
	)
}

func TestVerifyCaptchaRejectsMissingGoCaptchaToken(t *testing.T) {
	svc, _, _ := newAuthServiceForGoCaptchaTest(t, goCaptchaSettings("click"))

	err := svc.VerifyCaptcha(context.Background(), CaptchaProof{}, "203.0.113.10")

	require.ErrorIs(t, err, ErrGoCaptchaVerificationFailed)
}

func TestVerifyCaptchaRejectsGoCaptchaConflictingWithTurnstile(t *testing.T) {
	settings := goCaptchaSettings("click")
	settings[SettingKeyTurnstileEnabled] = "true"
	settings[SettingKeyTurnstileSecretKey] = "secret"
	svc, _, _ := newAuthServiceForGoCaptchaTest(t, settings)

	err := svc.VerifyCaptcha(context.Background(), CaptchaProof{TurnstileToken: "anything"}, "203.0.113.10")

	require.ErrorIs(t, err, ErrCaptchaProviderConflict)
}

func TestVerifyCaptchaRejectsGoCaptchaConflictingWithTencent(t *testing.T) {
	settings := goCaptchaSettings("click")
	for key, value := range tencentCaptchaSettings() {
		settings[key] = value
	}
	svc, _, _ := newAuthServiceForGoCaptchaTest(t, settings)

	err := svc.VerifyCaptcha(context.Background(), CaptchaProof{TurnstileToken: "anything"}, "203.0.113.10")

	require.ErrorIs(t, err, ErrCaptchaProviderConflict)
}

func TestVerifyActionCaptchaInterceptsWhenGoCaptchaEnabled(t *testing.T) {
	svc, goCaptchaService, _ := newAuthServiceForGoCaptchaTest(t, goCaptchaSettings("slide"))
	ctx := context.Background()

	// 自建验证码是动作触发式，与腾讯/阿里云同类，OAuth start 与 Passkey begin 也要拦
	require.ErrorIs(t,
		svc.VerifyActionCaptchaIfEnabled(ctx, CaptchaProof{TurnstileToken: "bogus"}, "203.0.113.10"),
		ErrGoCaptchaVerificationFailed,
	)

	token := issueGoCaptchaToken(t, goCaptchaService, "203.0.113.10")
	require.NoError(t, svc.VerifyActionCaptchaIfEnabled(ctx, CaptchaProof{TurnstileToken: token}, "203.0.113.10"))
}

func TestVerifyActionCaptchaSkipsWhenNoActionProviderEnabled(t *testing.T) {
	svc, _, _ := newAuthServiceForGoCaptchaTest(t, map[string]string{})

	err := svc.VerifyActionCaptchaIfEnabled(context.Background(), CaptchaProof{}, "203.0.113.10")

	require.NoError(t, err)
}

func TestVerifyCaptchaFailsClosedWhenGoCaptchaServiceMissing(t *testing.T) {
	cfg := &config.Config{Server: config.ServerConfig{Mode: "release"}}
	settingService := NewSettingService(&settingRepoStub{values: goCaptchaSettings("click")}, cfg)
	svc := NewAuthService(nil, &userRepoStub{}, nil, nil, cfg, settingService, nil, nil, nil, nil, nil, nil, nil)

	err := svc.VerifyCaptcha(context.Background(), CaptchaProof{TurnstileToken: "token"}, "203.0.113.10")

	require.ErrorIs(t, err, ErrGoCaptchaUnavailable)
}
