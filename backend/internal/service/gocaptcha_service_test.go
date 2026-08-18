//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// goCaptchaGeneratorStub 用固定答案替代真实图像生成，让 service 层的三阶段
// 流程可以脱离 go-captcha 与素材文件单独验证。
type goCaptchaGeneratorStub struct {
	challenge *GoCaptchaChallenge
	genErr    error
	valid     bool

	generateCalls  int
	lastMode       GoCaptchaMode
	lastPadding    int
	lastSubmission string
}

func (g *goCaptchaGeneratorStub) Generate(mode GoCaptchaMode) (*GoCaptchaChallenge, error) {
	g.generateCalls++
	g.lastMode = mode
	if g.genErr != nil {
		return nil, g.genErr
	}
	if g.challenge != nil {
		return g.challenge, nil
	}
	return &GoCaptchaChallenge{
		MasterImage: "data:image/jpeg;base64,master",
		ThumbImage:  "data:image/png;base64,thumb",
		Answer:      json.RawMessage(`{"answer":1}`),
	}, nil
}

func (g *goCaptchaGeneratorStub) Validate(mode GoCaptchaMode, _ json.RawMessage, submission string, padding int) bool {
	g.lastMode = mode
	g.lastPadding = padding
	g.lastSubmission = submission
	return g.valid
}

// goCaptchaCacheStub 是内存版 GoCaptchaCache，Take* 取出即删，
// 与 Redis GETDEL 的语义保持一致。
type goCaptchaCacheStub struct {
	challenges map[string][]byte
	tokens     map[string][]byte
	failures   map[string]int
	cooldowns  map[string]bool

	saveChallengeErr error
	takeChallengeErr error
	cooldownErr      error
	recordFailureErr error
	clearFailuresErr error
}

func newGoCaptchaCacheStub() *goCaptchaCacheStub {
	return &goCaptchaCacheStub{
		challenges: map[string][]byte{},
		tokens:     map[string][]byte{},
		failures:   map[string]int{},
		cooldowns:  map[string]bool{},
	}
}

func (c *goCaptchaCacheStub) SaveChallenge(_ context.Context, id string, payload []byte, _ time.Duration) error {
	if c.saveChallengeErr != nil {
		return c.saveChallengeErr
	}
	c.challenges[id] = payload
	return nil
}

func (c *goCaptchaCacheStub) TakeChallenge(_ context.Context, id string) ([]byte, error) {
	if c.takeChallengeErr != nil {
		return nil, c.takeChallengeErr
	}
	payload, ok := c.challenges[id]
	if !ok {
		return nil, nil
	}
	delete(c.challenges, id)
	return payload, nil
}

func (c *goCaptchaCacheStub) SaveToken(_ context.Context, hash string, payload []byte, _ time.Duration) error {
	c.tokens[hash] = payload
	return nil
}

func (c *goCaptchaCacheStub) TakeToken(_ context.Context, hash string) ([]byte, error) {
	payload, ok := c.tokens[hash]
	if !ok {
		return nil, nil
	}
	delete(c.tokens, hash)
	return payload, nil
}

func (c *goCaptchaCacheStub) IsCoolingDown(_ context.Context, ip string) (bool, error) {
	if c.cooldownErr != nil {
		return false, c.cooldownErr
	}
	return c.cooldowns[ip], nil
}

func (c *goCaptchaCacheStub) RecordFailure(_ context.Context, ip string, maxFailures int, _, _ time.Duration) (int, bool, error) {
	if c.recordFailureErr != nil {
		return 0, false, c.recordFailureErr
	}
	c.failures[ip]++
	if c.failures[ip] >= maxFailures {
		count := c.failures[ip]
		c.cooldowns[ip] = true
		delete(c.failures, ip)
		return count, true, nil
	}
	return c.failures[ip], false, nil
}

func (c *goCaptchaCacheStub) ClearFailures(_ context.Context, ip string) error {
	if c.clearFailuresErr != nil {
		return c.clearFailuresErr
	}
	delete(c.failures, ip)
	return nil
}

func goCaptchaSettings(mode string) map[string]string {
	settings := map[string]string{SettingKeyGoCaptchaEnabled: "true"}
	if mode != "" {
		settings[SettingKeyGoCaptchaMode] = mode
	}
	return settings
}

func newGoCaptchaServiceForTest(
	t *testing.T,
	settings map[string]string,
	generator *goCaptchaGeneratorStub,
	cache *goCaptchaCacheStub,
) *GoCaptchaService {
	t.Helper()
	cfg := &config.Config{
		GoCaptcha: config.GoCaptchaConfig{
			ChallengeTTL:  time.Minute,
			TokenTTL:      5 * time.Minute,
			PaddingClick:  5,
			PaddingSlide:  5,
			PaddingRotate: 8,
			MaxFailures:   3,
			FailureWindow: 10 * time.Minute,
			Cooldown:      10 * time.Minute,
		},
	}
	settingService := NewSettingService(&settingRepoStub{values: settings}, cfg)
	return NewGoCaptchaService(settingService, generator, cache, cfg)
}

func TestGoCaptchaCreateChallengeRejectedWhenDisabled(t *testing.T) {
	generator := &goCaptchaGeneratorStub{}
	svc := newGoCaptchaServiceForTest(t, map[string]string{}, generator, newGoCaptchaCacheStub())

	_, err := svc.CreateChallenge(context.Background(), "203.0.113.10")

	require.ErrorIs(t, err, ErrGoCaptchaNotEnabled)
	require.Zero(t, generator.generateCalls)
}

func TestGoCaptchaCreateChallengeFailsClosedWhenCooldownReadFails(t *testing.T) {
	generator := &goCaptchaGeneratorStub{}
	cache := newGoCaptchaCacheStub()
	cache.cooldownErr = errors.New("redis unavailable")
	svc := newGoCaptchaServiceForTest(t, goCaptchaSettings("click"), generator, cache)

	_, err := svc.CreateChallenge(context.Background(), "203.0.113.10")

	require.ErrorIs(t, err, ErrGoCaptchaUnavailable)
	require.Zero(t, generator.generateCalls)
}

func TestGoCaptchaCreateChallengeDoesNotLeakAnswer(t *testing.T) {
	generator := &goCaptchaGeneratorStub{
		challenge: &GoCaptchaChallenge{
			MasterImage: "master",
			ThumbImage:  "tile",
			TileX:       12,
			TileY:       84,
			TileWidth:   62,
			TileHeight:  62,
			Answer:      json.RawMessage(`{"x":211,"y":84}`),
		},
	}
	svc := newGoCaptchaServiceForTest(t, goCaptchaSettings("slide"), generator, newGoCaptchaCacheStub())

	view, err := svc.CreateChallenge(context.Background(), "203.0.113.10")
	require.NoError(t, err)

	encoded, err := json.Marshal(view)
	require.NoError(t, err)
	// 下发的只能是拼图块起始位置，缺口坐标必须留在服务端
	require.NotContains(t, string(encoded), "211")
	require.Contains(t, string(encoded), `"tile_x":12`)
}

func TestGoCaptchaChallengeIsSingleUse(t *testing.T) {
	generator := &goCaptchaGeneratorStub{valid: true}
	cache := newGoCaptchaCacheStub()
	svc := newGoCaptchaServiceForTest(t, goCaptchaSettings("click"), generator, cache)
	ctx := context.Background()

	view, err := svc.CreateChallenge(ctx, "203.0.113.10")
	require.NoError(t, err)

	token, err := svc.SolveChallenge(ctx, view.CaptchaID, "1,2,3,4,5,6", "203.0.113.10")
	require.NoError(t, err)
	require.NotEmpty(t, token)

	// 同一个挑战不能作答两次，即便第一次是答对的
	_, err = svc.SolveChallenge(ctx, view.CaptchaID, "1,2,3,4,5,6", "203.0.113.10")
	require.ErrorIs(t, err, ErrGoCaptchaVerificationFailed)
}

func TestGoCaptchaWrongAnswerConsumesChallenge(t *testing.T) {
	generator := &goCaptchaGeneratorStub{valid: false}
	cache := newGoCaptchaCacheStub()
	svc := newGoCaptchaServiceForTest(t, goCaptchaSettings("click"), generator, cache)
	ctx := context.Background()

	view, err := svc.CreateChallenge(ctx, "203.0.113.10")
	require.NoError(t, err)

	_, err = svc.SolveChallenge(ctx, view.CaptchaID, "0,0", "203.0.113.10")
	require.ErrorIs(t, err, ErrGoCaptchaVerificationFailed)

	// 答错之后挑战同样已被取走，重试必须重新申请
	require.Empty(t, cache.challenges)
	require.Equal(t, 1, cache.failures["203.0.113.10"])
}

func TestGoCaptchaWrongAnswerFailsClosedWhenFailureCannotBeRecorded(t *testing.T) {
	generator := &goCaptchaGeneratorStub{valid: false}
	cache := newGoCaptchaCacheStub()
	cache.recordFailureErr = errors.New("redis unavailable")
	svc := newGoCaptchaServiceForTest(t, goCaptchaSettings("slide"), generator, cache)
	ctx := context.Background()

	view, err := svc.CreateChallenge(ctx, "203.0.113.10")
	require.NoError(t, err)
	_, err = svc.SolveChallenge(ctx, view.CaptchaID, "0,0", "203.0.113.10")

	require.ErrorIs(t, err, ErrGoCaptchaUnavailable)
}

func TestGoCaptchaCorrectAnswerFailsClosedWhenFailureCounterCannotBeCleared(t *testing.T) {
	generator := &goCaptchaGeneratorStub{valid: true}
	cache := newGoCaptchaCacheStub()
	cache.clearFailuresErr = errors.New("redis unavailable")
	svc := newGoCaptchaServiceForTest(t, goCaptchaSettings("click"), generator, cache)
	ctx := context.Background()

	view, err := svc.CreateChallenge(ctx, "203.0.113.10")
	require.NoError(t, err)
	_, err = svc.SolveChallenge(ctx, view.CaptchaID, "1,2,3,4,5,6", "203.0.113.10")

	require.ErrorIs(t, err, ErrGoCaptchaUnavailable)
	require.Empty(t, cache.tokens)
}

func TestGoCaptchaTokenIsSingleUse(t *testing.T) {
	generator := &goCaptchaGeneratorStub{valid: true}
	svc := newGoCaptchaServiceForTest(t, goCaptchaSettings("click"), generator, newGoCaptchaCacheStub())
	ctx := context.Background()

	view, err := svc.CreateChallenge(ctx, "203.0.113.10")
	require.NoError(t, err)
	token, err := svc.SolveChallenge(ctx, view.CaptchaID, "1,2,3,4,5,6", "203.0.113.10")
	require.NoError(t, err)

	require.NoError(t, svc.ConsumeToken(ctx, token, "203.0.113.10"))
	require.ErrorIs(t, svc.ConsumeToken(ctx, token, "203.0.113.10"), ErrGoCaptchaVerificationFailed)
}

func TestGoCaptchaMetricsTrackModeSpecificLifecycle(t *testing.T) {
	generator := &goCaptchaGeneratorStub{valid: true}
	svc := newGoCaptchaServiceForTest(t, goCaptchaSettings("drag"), generator, newGoCaptchaCacheStub())
	ctx := context.Background()
	before := GoCaptchaMetricsSnapshot()[string(GoCaptchaModeDrag)]

	view, err := svc.CreateChallenge(ctx, "203.0.113.10")
	require.NoError(t, err)
	token, err := svc.SolveChallenge(ctx, view.CaptchaID, "10,20", "203.0.113.10")
	require.NoError(t, err)
	require.NoError(t, svc.ConsumeToken(ctx, token, "203.0.113.10"))

	after := GoCaptchaMetricsSnapshot()[string(GoCaptchaModeDrag)]
	require.Equal(t, before.ChallengeGenerated+1, after.ChallengeGenerated)
	require.Greater(t, after.ChallengeGenerationDurationNs, before.ChallengeGenerationDurationNs)
	require.Equal(t, before.VerifySucceeded+1, after.VerifySucceeded)
	require.Equal(t, before.TokenConsumeSucceeded+1, after.TokenConsumeSucceeded)
}

func TestGoCaptchaConsumeTokenRejectsUnknownToken(t *testing.T) {
	svc := newGoCaptchaServiceForTest(t, goCaptchaSettings("click"), &goCaptchaGeneratorStub{}, newGoCaptchaCacheStub())

	require.ErrorIs(t, svc.ConsumeToken(context.Background(), "", "203.0.113.10"), ErrGoCaptchaVerificationFailed)
	require.ErrorIs(t, svc.ConsumeToken(context.Background(), "not-a-token", "203.0.113.10"), ErrGoCaptchaVerificationFailed)
}

func TestGoCaptchaExpiredChallengeReturnsGenericFailure(t *testing.T) {
	generator := &goCaptchaGeneratorStub{valid: true}
	cache := newGoCaptchaCacheStub()
	svc := newGoCaptchaServiceForTest(t, goCaptchaSettings("click"), generator, cache)
	ctx := context.Background()

	view, err := svc.CreateChallenge(ctx, "203.0.113.10")
	require.NoError(t, err)
	// 模拟 TTL 到期
	delete(cache.challenges, view.CaptchaID)

	_, err = svc.SolveChallenge(ctx, view.CaptchaID, "1,2,3,4,5,6", "203.0.113.10")
	// 过期与答错必须返回同一个错误码，不给攻击者区分信号
	require.ErrorIs(t, err, ErrGoCaptchaVerificationFailed)
}

func TestGoCaptchaCooldownBlocksNewChallenges(t *testing.T) {
	generator := &goCaptchaGeneratorStub{valid: false}
	cache := newGoCaptchaCacheStub()
	svc := newGoCaptchaServiceForTest(t, goCaptchaSettings("slide"), generator, cache)
	ctx := context.Background()
	const clientIP = "203.0.113.10"

	// MaxFailures 为 3，连续答错三次后进入冷却
	for i := 0; i < 3; i++ {
		view, err := svc.CreateChallenge(ctx, clientIP)
		require.NoError(t, err)
		_, err = svc.SolveChallenge(ctx, view.CaptchaID, "0,0", clientIP)
		require.ErrorIs(t, err, ErrGoCaptchaVerificationFailed)
	}

	_, err := svc.CreateChallenge(ctx, clientIP)
	require.ErrorIs(t, err, ErrGoCaptchaTooManyFailures)

	// 其他 IP 不受影响
	_, err = svc.CreateChallenge(ctx, "198.51.100.7")
	require.NoError(t, err)
}

func TestGoCaptchaSuccessClearsFailureCounter(t *testing.T) {
	generator := &goCaptchaGeneratorStub{valid: false}
	cache := newGoCaptchaCacheStub()
	svc := newGoCaptchaServiceForTest(t, goCaptchaSettings("slide"), generator, cache)
	ctx := context.Background()
	const clientIP = "203.0.113.10"

	view, err := svc.CreateChallenge(ctx, clientIP)
	require.NoError(t, err)
	_, err = svc.SolveChallenge(ctx, view.CaptchaID, "0,0", clientIP)
	require.ErrorIs(t, err, ErrGoCaptchaVerificationFailed)
	require.Equal(t, 1, cache.failures[clientIP])

	generator.valid = true
	view, err = svc.CreateChallenge(ctx, clientIP)
	require.NoError(t, err)
	_, err = svc.SolveChallenge(ctx, view.CaptchaID, "10,20", clientIP)
	require.NoError(t, err)
	require.NotContains(t, cache.failures, clientIP)
}

func TestGoCaptchaBindIPRejectsMismatchedConsumer(t *testing.T) {
	generator := &goCaptchaGeneratorStub{valid: true}
	cfg := &config.Config{GoCaptcha: config.GoCaptchaConfig{BindIP: true}}
	settingService := NewSettingService(&settingRepoStub{values: goCaptchaSettings("click")}, cfg)
	svc := NewGoCaptchaService(settingService, generator, newGoCaptchaCacheStub(), cfg)
	ctx := context.Background()

	view, err := svc.CreateChallenge(ctx, "203.0.113.10")
	require.NoError(t, err)
	token, err := svc.SolveChallenge(ctx, view.CaptchaID, "1,2,3,4,5,6", "203.0.113.10")
	require.NoError(t, err)

	require.ErrorIs(t, svc.ConsumeToken(ctx, token, "198.51.100.7"), ErrGoCaptchaVerificationFailed)
}

func TestGoCaptchaBindIPDisabledAllowsDifferentConsumerIP(t *testing.T) {
	generator := &goCaptchaGeneratorStub{valid: true}
	svc := newGoCaptchaServiceForTest(t, goCaptchaSettings("click"), generator, newGoCaptchaCacheStub())
	ctx := context.Background()

	view, err := svc.CreateChallenge(ctx, "203.0.113.10")
	require.NoError(t, err)
	token, err := svc.SolveChallenge(ctx, view.CaptchaID, "1,2,3,4,5,6", "203.0.113.10")
	require.NoError(t, err)

	require.NoError(t, svc.ConsumeToken(ctx, token, "198.51.100.7"))
}

func TestGoCaptchaGenerateFailureIsNotLeakedToClient(t *testing.T) {
	generator := &goCaptchaGeneratorStub{genErr: errors.New("font missing")}
	svc := newGoCaptchaServiceForTest(t, goCaptchaSettings("click"), generator, newGoCaptchaCacheStub())

	_, err := svc.CreateChallenge(context.Background(), "203.0.113.10")

	require.ErrorIs(t, err, ErrGoCaptchaUnavailable)
}

func TestGoCaptchaPaddingIsModeSpecific(t *testing.T) {
	cases := []struct {
		mode    string
		padding int
	}{
		{"click", 5},
		{"shape", 5},
		{"slide", 5},
		{"drag", 5},
		{"rotate", 8},
	}

	for _, tc := range cases {
		t.Run(tc.mode, func(t *testing.T) {
			generator := &goCaptchaGeneratorStub{valid: true}
			svc := newGoCaptchaServiceForTest(t, goCaptchaSettings(tc.mode), generator, newGoCaptchaCacheStub())
			ctx := context.Background()

			view, err := svc.CreateChallenge(ctx, "203.0.113.10")
			require.NoError(t, err)
			require.Equal(t, GoCaptchaMode(tc.mode), view.Mode)

			_, err = svc.SolveChallenge(ctx, view.CaptchaID, "1,2", "203.0.113.10")
			require.NoError(t, err)
			require.Equal(t, tc.padding, generator.lastPadding)
		})
	}
}

func TestNormalizeGoCaptchaModeFallsBackToClick(t *testing.T) {
	require.Equal(t, GoCaptchaModeClick, NormalizeGoCaptchaMode(""))
	require.Equal(t, GoCaptchaModeClick, NormalizeGoCaptchaMode("nonsense"))
	require.Equal(t, GoCaptchaModeShape, NormalizeGoCaptchaMode(" shape "))
	require.Equal(t, GoCaptchaModeSlide, NormalizeGoCaptchaMode("slide"))
	require.Equal(t, GoCaptchaModeDrag, NormalizeGoCaptchaMode("drag"))
	require.Equal(t, GoCaptchaModeRotate, NormalizeGoCaptchaMode("rotate"))
}

func TestGoCaptchaUnknownStoredModeStillGeneratesClick(t *testing.T) {
	generator := &goCaptchaGeneratorStub{valid: true}
	svc := newGoCaptchaServiceForTest(t, goCaptchaSettings("garbage-from-db"), generator, newGoCaptchaCacheStub())

	view, err := svc.CreateChallenge(context.Background(), "203.0.113.10")

	require.NoError(t, err)
	require.Equal(t, GoCaptchaModeClick, view.Mode)
	require.Equal(t, GoCaptchaModeClick, generator.lastMode)
}
