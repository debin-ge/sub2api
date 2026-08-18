package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

const (
	// captchaVerifyBodyMaxBytes is deliberately tiny compared with the global
	// request limit: a valid captcha proof contains only a short ID and a handful
	// of integer coordinates. Keeping this endpoint bounded prevents anonymous
	// callers from using JSON decoding as a memory/CPU amplification primitive.
	captchaVerifyBodyMaxBytes int64 = 8 * 1024
)

// CaptchaHandler 自建行为验证码的公开接口（无需认证）。
type CaptchaHandler struct {
	goCaptchaService *service.GoCaptchaService
}

func NewCaptchaHandler(goCaptchaService *service.GoCaptchaService) *CaptchaHandler {
	return &CaptchaHandler{goCaptchaService: goCaptchaService}
}

// CaptchaVerifyRequest 五种交互模式共用同一个 answer 字符串字段，
// 切换模式时请求契约保持稳定：
//   - click / shape : "x1,y1,x2,y2,..."，按点击顺序
//   - slide / drag  : "x,y"
//   - rotate        : "angle"
type CaptchaVerifyRequest struct {
	CaptchaID string `json:"captcha_id" binding:"required,max=128"`
	Answer    string `json:"answer" binding:"required,max=4096"`
}

// CaptchaVerifyResponse 作答通过后签发的一次性令牌。
type CaptchaVerifyResponse struct {
	Token     string `json:"token"`
	ExpiresIn int    `json:"expires_in"`
}

// CreateChallenge 申请一道验证题
// POST /api/v1/captcha/challenge
func (h *CaptchaHandler) CreateChallenge(c *gin.Context) {
	if h.goCaptchaService == nil {
		response.ErrorFrom(c, service.ErrGoCaptchaUnavailable)
		return
	}

	challenge, err := h.goCaptchaService.CreateChallenge(c.Request.Context(), ip.GetClientIP(c))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, challenge)
}

// Verify 提交作答，通过后换取一次性令牌
// POST /api/v1/captcha/verify
func (h *CaptchaHandler) Verify(c *gin.Context) {
	if h.goCaptchaService == nil {
		response.ErrorFrom(c, service.ErrGoCaptchaUnavailable)
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, captchaVerifyBodyMaxBytes)

	var req CaptchaVerifyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// 参数不合法与答案错误对外表现一致，避免给攻击者提供区分信号
		response.ErrorFrom(c, service.ErrGoCaptchaVerificationFailed)
		return
	}

	token, err := h.goCaptchaService.SolveChallenge(c.Request.Context(), req.CaptchaID, req.Answer, ip.GetClientIP(c))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, CaptchaVerifyResponse{
		Token:     token,
		ExpiresIn: h.goCaptchaService.TokenTTLSeconds(),
	})
}
