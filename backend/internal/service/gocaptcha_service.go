package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

var (
	// ErrGoCaptchaVerificationFailed 涵盖答案错误、挑战不存在、挑战已过期、挑战已被消费四种情况。
	// 刻意不区分，避免给攻击者提供可用于筛选的信号。
	ErrGoCaptchaVerificationFailed = infraerrors.BadRequest("GO_CAPTCHA_VERIFICATION_FAILED", "captcha verification failed")
	ErrGoCaptchaTooManyFailures    = infraerrors.TooManyRequests("GO_CAPTCHA_TOO_MANY_FAILURES", "too many failed attempts, please try again later")
	ErrGoCaptchaNotEnabled         = infraerrors.ServiceUnavailable("GO_CAPTCHA_NOT_ENABLED", "self-hosted captcha not enabled")
	ErrGoCaptchaUnavailable        = infraerrors.ServiceUnavailable("GO_CAPTCHA_UNAVAILABLE", "self-hosted captcha unavailable")
)

// GoCaptchaMode 自建行为验证码的交互模式。
type GoCaptchaMode string

const (
	GoCaptchaModeClick  GoCaptchaMode = "click"  // 文字点选（默认）
	GoCaptchaModeShape  GoCaptchaMode = "shape"  // 图形点选，与语言无关
	GoCaptchaModeSlide  GoCaptchaMode = "slide"  // 滑动
	GoCaptchaModeDrag   GoCaptchaMode = "drag"   // 拖拽
	GoCaptchaModeRotate GoCaptchaMode = "rotate" // 旋转
)

// NormalizeGoCaptchaMode 未知值一律回落到默认的文字点选，
// 避免数据库里的脏值让验证码整体不可用。
func NormalizeGoCaptchaMode(v string) GoCaptchaMode {
	switch GoCaptchaMode(strings.TrimSpace(v)) {
	case GoCaptchaModeShape:
		return GoCaptchaModeShape
	case GoCaptchaModeSlide:
		return GoCaptchaModeSlide
	case GoCaptchaModeDrag:
		return GoCaptchaModeDrag
	case GoCaptchaModeRotate:
		return GoCaptchaModeRotate
	default:
		return GoCaptchaModeClick
	}
}

// GoCaptchaChallenge 由 generator 产出。
// Answer 只在服务端流转，任何情况下都不得进入 HTTP 响应。
// 各模式共用此结构，用不到的几何字段留零值。
type GoCaptchaChallenge struct {
	MasterImage string
	ThumbImage  string
	TileX       int // slide / drag：拼图块的起始摆放位置，不是答案
	TileY       int
	TileWidth   int
	TileHeight  int
	ThumbSize   int // rotate：缩略图直径
	Answer      json.RawMessage
}

// GoCaptchaGenerator 行为验证码的生成与几何校验端口，实现在 repository 层。
// 答案以不透明的 json.RawMessage 在 service 层过手，因此 service 不需要
// 理解坐标几何，也不需要 import go-captcha。
type GoCaptchaGenerator interface {
	Generate(mode GoCaptchaMode) (*GoCaptchaChallenge, error)
	Validate(mode GoCaptchaMode, answer json.RawMessage, submission string, padding int) bool
}

// GoCaptchaCache 挑战、令牌与失败计数的短期存储端口，实现在 repository 层（Redis）。
type GoCaptchaCache interface {
	SaveChallenge(ctx context.Context, id string, payload []byte, ttl time.Duration) error
	// TakeChallenge 原子取出并删除，保证一个挑战只有一次作答机会。
	TakeChallenge(ctx context.Context, id string) ([]byte, error)
	SaveToken(ctx context.Context, hash string, payload []byte, ttl time.Duration) error
	// TakeToken 原子取出并删除，保证令牌一次性消费。
	TakeToken(ctx context.Context, hash string) ([]byte, error)

	IsCoolingDown(ctx context.Context, ip string) (bool, error)
	RecordFailure(ctx context.Context, ip string, maxFailures int, window, cooldown time.Duration) (count int, cooled bool, err error)
	ClearFailures(ctx context.Context, ip string) error
}

type goCaptchaStoredChallenge struct {
	Mode   GoCaptchaMode   `json:"mode"`
	Answer json.RawMessage `json:"answer"`
	IP     string          `json:"ip,omitempty"`
}

type goCaptchaStoredToken struct {
	IP        string        `json:"ip,omitempty"`
	Mode      GoCaptchaMode `json:"mode,omitempty"`
	CreatedAt int64         `json:"created_at"`
}

// GoCaptchaMetricSnapshot exposes process-local counters for ops collection.
// Counters are separated by mode; malformed/unknown proofs use the "unknown"
// bucket so failures are still observable without inventing a mode.
type GoCaptchaMetricSnapshot struct {
	ChallengeGenerated            int64
	ChallengeGenerationFailed     int64
	ChallengeGenerationDurationNs int64
	ChallengeGenerationMaxNs      int64
	VerifySucceeded               int64
	VerifyFailed                  int64
	TokenConsumeSucceeded         int64
	TokenConsumeFailed            int64
}

type goCaptchaMetricCounters struct {
	challengeGenerated            atomic.Int64
	challengeGenerationFailed     atomic.Int64
	challengeGenerationDurationNs atomic.Int64
	challengeGenerationMaxNs      atomic.Int64
	verifySucceeded               atomic.Int64
	verifyFailed                  atomic.Int64
	tokenConsumeSucceeded         atomic.Int64
	tokenConsumeFailed            atomic.Int64
}

var goCaptchaMetrics = struct {
	click   goCaptchaMetricCounters
	shape   goCaptchaMetricCounters
	slide   goCaptchaMetricCounters
	drag    goCaptchaMetricCounters
	rotate  goCaptchaMetricCounters
	unknown goCaptchaMetricCounters
}{}

func goCaptchaMetricForMode(mode GoCaptchaMode) *goCaptchaMetricCounters {
	switch mode {
	case GoCaptchaModeClick:
		return &goCaptchaMetrics.click
	case GoCaptchaModeShape:
		return &goCaptchaMetrics.shape
	case GoCaptchaModeSlide:
		return &goCaptchaMetrics.slide
	case GoCaptchaModeDrag:
		return &goCaptchaMetrics.drag
	case GoCaptchaModeRotate:
		return &goCaptchaMetrics.rotate
	default:
		return &goCaptchaMetrics.unknown
	}
}

func (m *goCaptchaMetricCounters) recordGeneration(duration time.Duration, success bool) {
	nanoseconds := duration.Nanoseconds()
	m.challengeGenerationDurationNs.Add(nanoseconds)
	for {
		current := m.challengeGenerationMaxNs.Load()
		if nanoseconds <= current || m.challengeGenerationMaxNs.CompareAndSwap(current, nanoseconds) {
			break
		}
	}
	if success {
		m.challengeGenerated.Add(1)
	} else {
		m.challengeGenerationFailed.Add(1)
	}
}

func (m *goCaptchaMetricCounters) snapshot() GoCaptchaMetricSnapshot {
	return GoCaptchaMetricSnapshot{
		ChallengeGenerated:            m.challengeGenerated.Load(),
		ChallengeGenerationFailed:     m.challengeGenerationFailed.Load(),
		ChallengeGenerationDurationNs: m.challengeGenerationDurationNs.Load(),
		ChallengeGenerationMaxNs:      m.challengeGenerationMaxNs.Load(),
		VerifySucceeded:               m.verifySucceeded.Load(),
		VerifyFailed:                  m.verifyFailed.Load(),
		TokenConsumeSucceeded:         m.tokenConsumeSucceeded.Load(),
		TokenConsumeFailed:            m.tokenConsumeFailed.Load(),
	}
}

// GoCaptchaMetricsSnapshot returns a stable copy suitable for an ops endpoint
// or collector. Values are process-local and monotonically increasing.
func GoCaptchaMetricsSnapshot() map[string]GoCaptchaMetricSnapshot {
	return map[string]GoCaptchaMetricSnapshot{
		string(GoCaptchaModeClick):  goCaptchaMetrics.click.snapshot(),
		string(GoCaptchaModeShape):  goCaptchaMetrics.shape.snapshot(),
		string(GoCaptchaModeSlide):  goCaptchaMetrics.slide.snapshot(),
		string(GoCaptchaModeDrag):   goCaptchaMetrics.drag.snapshot(),
		string(GoCaptchaModeRotate): goCaptchaMetrics.rotate.snapshot(),
		"unknown":                   goCaptchaMetrics.unknown.snapshot(),
	}
}

// GoCaptchaChallengeView 下发给前端的挑战数据，不含答案。
type GoCaptchaChallengeView struct {
	CaptchaID   string        `json:"captcha_id"`
	Mode        GoCaptchaMode `json:"mode"`
	MasterImage string        `json:"master_image"`
	ThumbImage  string        `json:"thumb_image"`
	TileX       int           `json:"tile_x,omitempty"`
	TileY       int           `json:"tile_y,omitempty"`
	TileWidth   int           `json:"tile_width,omitempty"`
	TileHeight  int           `json:"tile_height,omitempty"`
	ThumbSize   int           `json:"thumb_size,omitempty"`
}

// GoCaptchaService 编排自建验证码的三阶段流程：
// 申请挑战 -> 提交作答换取一次性令牌 -> 业务请求消费令牌。
type GoCaptchaService struct {
	settingService *SettingService
	generator      GoCaptchaGenerator
	cache          GoCaptchaCache
	cfg            *config.Config
}

func NewGoCaptchaService(
	settingService *SettingService,
	generator GoCaptchaGenerator,
	cache GoCaptchaCache,
	cfg *config.Config,
) *GoCaptchaService {
	return &GoCaptchaService{
		settingService: settingService,
		generator:      generator,
		cache:          cache,
		cfg:            cfg,
	}
}

// CreateChallenge 生成一道验证题，答案写入 Redis，只把图片与布局参数返回给前端。
func (s *GoCaptchaService) CreateChallenge(ctx context.Context, remoteIP string) (*GoCaptchaChallengeView, error) {
	if s == nil || s.generator == nil || s.cache == nil || s.settingService == nil {
		return nil, ErrGoCaptchaUnavailable
	}

	providerConfig, err := s.settingService.GetCaptchaProviderConfig(ctx)
	if err != nil {
		logger.LegacyPrintf("service.gocaptcha", "%s", "[GoCaptcha] Failed to read captcha provider settings")
		return nil, ErrServiceUnavailable
	}
	if !providerConfig.GoCaptcha.Enabled {
		return nil, ErrGoCaptchaNotEnabled
	}
	mode := providerConfig.GoCaptcha.Mode

	// 连续失败冷却。滑动与旋转的单次盲猜成功率在 1/20 量级，
	// 仅靠速率限制不足以拦住爆破，冷却是这两种模式的主要防线。
	cooling, err := s.cache.IsCoolingDown(ctx, remoteIP)
	if err != nil {
		logger.LegacyPrintf("service.gocaptcha", "[GoCaptcha] Failed to read cooldown state: %v", err)
		return nil, ErrGoCaptchaUnavailable
	}
	if cooling {
		return nil, ErrGoCaptchaTooManyFailures
	}

	generateStartedAt := time.Now()
	challenge, err := s.generator.Generate(mode)
	goCaptchaMetricForMode(mode).recordGeneration(time.Since(generateStartedAt), err == nil)
	if err != nil {
		logger.LegacyPrintf("service.gocaptcha", "[GoCaptcha] Generate failed: %v", err)
		return nil, ErrGoCaptchaUnavailable
	}

	id, err := newGoCaptchaID()
	if err != nil {
		return nil, ErrGoCaptchaUnavailable
	}
	payload, err := json.Marshal(goCaptchaStoredChallenge{
		Mode:   mode,
		Answer: challenge.Answer,
		IP:     s.boundIP(remoteIP),
	})
	if err != nil {
		return nil, ErrGoCaptchaUnavailable
	}
	if err := s.cache.SaveChallenge(ctx, id, payload, s.challengeTTL()); err != nil {
		logger.LegacyPrintf("service.gocaptcha", "[GoCaptcha] Failed to persist challenge: %v", err)
		return nil, ErrGoCaptchaUnavailable
	}

	return &GoCaptchaChallengeView{
		CaptchaID:   id,
		Mode:        mode,
		MasterImage: challenge.MasterImage,
		ThumbImage:  challenge.ThumbImage,
		TileX:       challenge.TileX,
		TileY:       challenge.TileY,
		TileWidth:   challenge.TileWidth,
		TileHeight:  challenge.TileHeight,
		ThumbSize:   challenge.ThumbSize,
	}, nil
}

// SolveChallenge 校验作答并签发一次性令牌。
// 挑战无论答对答错都已被原子取走，答错必须重新申请挑战。
func (s *GoCaptchaService) SolveChallenge(ctx context.Context, id, submission, remoteIP string) (string, error) {
	if s == nil || s.generator == nil || s.cache == nil {
		return "", ErrGoCaptchaUnavailable
	}
	if strings.TrimSpace(id) == "" || strings.TrimSpace(submission) == "" {
		goCaptchaMetricForMode("").verifyFailed.Add(1)
		return "", ErrGoCaptchaVerificationFailed
	}
	cooling, err := s.cache.IsCoolingDown(ctx, remoteIP)
	if err != nil {
		logger.LegacyPrintf("service.gocaptcha", "[GoCaptcha] Failed to read cooldown state: %v", err)
		return "", ErrGoCaptchaUnavailable
	}
	if cooling {
		return "", ErrGoCaptchaTooManyFailures
	}

	raw, err := s.cache.TakeChallenge(ctx, id)
	if err != nil {
		logger.LegacyPrintf("service.gocaptcha", "[GoCaptcha] Failed to load challenge: %v", err)
		return "", ErrGoCaptchaUnavailable
	}
	if len(raw) == 0 {
		goCaptchaMetricForMode("").verifyFailed.Add(1)
		return "", ErrGoCaptchaVerificationFailed
	}

	var stored goCaptchaStoredChallenge
	if err := json.Unmarshal(raw, &stored); err != nil {
		goCaptchaMetricForMode("").verifyFailed.Add(1)
		return "", ErrGoCaptchaVerificationFailed
	}
	if stored.IP != "" && stored.IP != remoteIP {
		goCaptchaMetricForMode(stored.Mode).verifyFailed.Add(1)
		return "", ErrGoCaptchaVerificationFailed
	}

	if !s.generator.Validate(stored.Mode, stored.Answer, submission, s.padding(stored.Mode)) {
		goCaptchaMetricForMode(stored.Mode).verifyFailed.Add(1)
		if err := s.recordFailure(ctx, remoteIP); err != nil {
			return "", err
		}
		return "", ErrGoCaptchaVerificationFailed
	}
	if err := s.cache.ClearFailures(ctx, remoteIP); err != nil {
		logger.LegacyPrintf("service.gocaptcha", "[GoCaptcha] Failed to clear failure counter: %v", err)
		return "", ErrGoCaptchaUnavailable
	}

	token, err := newGoCaptchaToken()
	if err != nil {
		return "", ErrGoCaptchaUnavailable
	}
	tokenPayload, err := json.Marshal(goCaptchaStoredToken{
		IP:        s.boundIP(remoteIP),
		Mode:      stored.Mode,
		CreatedAt: time.Now().Unix(),
	})
	if err != nil {
		return "", ErrGoCaptchaUnavailable
	}
	if err := s.cache.SaveToken(ctx, hashGoCaptchaToken(token), tokenPayload, s.tokenTTL()); err != nil {
		logger.LegacyPrintf("service.gocaptcha", "[GoCaptcha] Failed to persist token: %v", err)
		return "", ErrGoCaptchaUnavailable
	}
	goCaptchaMetricForMode(stored.Mode).verifySucceeded.Add(1)
	return token, nil
}

// ConsumeToken 由 AuthService 在业务请求中调用，一次性消费令牌。
func (s *GoCaptchaService) ConsumeToken(ctx context.Context, token, remoteIP string) error {
	if s == nil || s.cache == nil {
		return ErrGoCaptchaUnavailable
	}
	if strings.TrimSpace(token) == "" {
		goCaptchaMetricForMode("").tokenConsumeFailed.Add(1)
		return ErrGoCaptchaVerificationFailed
	}

	raw, err := s.cache.TakeToken(ctx, hashGoCaptchaToken(token))
	if err != nil {
		logger.LegacyPrintf("service.gocaptcha", "[GoCaptcha] Failed to load token: %v", err)
		return ErrGoCaptchaUnavailable
	}
	if len(raw) == 0 {
		goCaptchaMetricForMode("").tokenConsumeFailed.Add(1)
		return ErrGoCaptchaVerificationFailed
	}

	var stored goCaptchaStoredToken
	if err := json.Unmarshal(raw, &stored); err != nil {
		goCaptchaMetricForMode("").tokenConsumeFailed.Add(1)
		return ErrGoCaptchaVerificationFailed
	}
	if stored.IP != "" && stored.IP != remoteIP {
		goCaptchaMetricForMode(stored.Mode).tokenConsumeFailed.Add(1)
		return ErrGoCaptchaVerificationFailed
	}
	goCaptchaMetricForMode(stored.Mode).tokenConsumeSucceeded.Add(1)
	return nil
}

// TokenTTLSeconds 供 handler 回传给前端，用于提示令牌何时失效。
func (s *GoCaptchaService) TokenTTLSeconds() int {
	return int(s.tokenTTL() / time.Second)
}

func (s *GoCaptchaService) recordFailure(ctx context.Context, remoteIP string) error {
	maxFailures, window, cooldown := s.failurePolicy()
	if maxFailures <= 0 {
		return nil
	}
	count, cooled, err := s.cache.RecordFailure(ctx, remoteIP, maxFailures, window, cooldown)
	if err != nil {
		logger.LegacyPrintf("service.gocaptcha", "[GoCaptcha] Failed to record failure: %v", err)
		return ErrGoCaptchaUnavailable
	}
	if cooled {
		logger.LegacyPrintf(
			"service.gocaptcha",
			"[GoCaptcha] WARN failure cooldown triggered: ip=%s failures=%d cooldown=%s",
			remoteIP,
			count,
			cooldown,
		)
	}
	return nil
}

// boundIP 未开启 IP 绑定时返回空串，此处返回空串会让后续所有 IP 比对自动跳过。
func (s *GoCaptchaService) boundIP(remoteIP string) string {
	if s.cfg == nil || !s.cfg.GoCaptcha.BindIP {
		return ""
	}
	return remoteIP
}

func (s *GoCaptchaService) challengeTTL() time.Duration {
	if s.cfg != nil && s.cfg.GoCaptcha.ChallengeTTL > 0 {
		return s.cfg.GoCaptcha.ChallengeTTL
	}
	return 120 * time.Second
}

func (s *GoCaptchaService) tokenTTL() time.Duration {
	if s.cfg != nil && s.cfg.GoCaptcha.TokenTTL > 0 {
		return s.cfg.GoCaptcha.TokenTTL
	}
	return 300 * time.Second
}

// padding 按模式返回容差。旋转的单位是度，其余是像素。
func (s *GoCaptchaService) padding(mode GoCaptchaMode) int {
	if s.cfg == nil {
		return defaultGoCaptchaPadding(mode)
	}
	var configured int
	switch mode {
	case GoCaptchaModeRotate:
		configured = s.cfg.GoCaptcha.PaddingRotate
	case GoCaptchaModeSlide, GoCaptchaModeDrag:
		configured = s.cfg.GoCaptcha.PaddingSlide
	default:
		configured = s.cfg.GoCaptcha.PaddingClick
	}
	if configured <= 0 {
		return defaultGoCaptchaPadding(mode)
	}
	return configured
}

func defaultGoCaptchaPadding(mode GoCaptchaMode) int {
	if mode == GoCaptchaModeRotate {
		return 8
	}
	return 5
}

func (s *GoCaptchaService) failurePolicy() (maxFailures int, window, cooldown time.Duration) {
	maxFailures, window, cooldown = 5, 10*time.Minute, 10*time.Minute
	if s.cfg == nil {
		return
	}
	if s.cfg.GoCaptcha.MaxFailures > 0 {
		maxFailures = s.cfg.GoCaptcha.MaxFailures
	}
	if s.cfg.GoCaptcha.FailureWindow > 0 {
		window = s.cfg.GoCaptcha.FailureWindow
	}
	if s.cfg.GoCaptcha.Cooldown > 0 {
		cooldown = s.cfg.GoCaptcha.Cooldown
	}
	return
}

// hashGoCaptchaToken Redis 里只留哈希，Redis 被 dump 也拿不到可用令牌。
func hashGoCaptchaToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func newGoCaptchaID() (string, error) {
	return randomURLSafeString(16)
}

func newGoCaptchaToken() (string, error) {
	return randomURLSafeString(32)
}

func randomURLSafeString(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate random bytes: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
