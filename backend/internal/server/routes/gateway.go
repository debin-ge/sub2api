package routes

import (
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// RegisterGatewayRoutes 注册 API 网关路由（Claude/OpenAI/Gemini 兼容）
func RegisterGatewayRoutes(
	r *gin.Engine,
	h *handler.Handlers,
	apiKeyAuth middleware.APIKeyAuthMiddleware,
	apiKeyService *service.APIKeyService,
	subscriptionService *service.SubscriptionService,
	opsService *service.OpsService,
	settingService *service.SettingService,
	cfg *config.Config,
) {
	bodyLimit := middleware.RequestBodyLimit(cfg.Gateway.MaxBodySize)
	clientRequestID := middleware.ClientRequestID()
	opsErrorLogger := handler.OpsErrorLoggerMiddleware(opsService)
	endpointNorm := handler.InboundEndpointMiddleware()

	// 未分组 Key 拦截中间件（按协议格式区分错误响应）
	requireGroupAnthropic := middleware.RequireGroupAssignment(settingService, middleware.AnthropicErrorWriter)
	requireGroupGoogle := middleware.RequireGroupAssignment(settingService, middleware.GoogleErrorWriter)

	// API网关（Claude API兼容）
	gateway := r.Group("/v1")
	gateway.Use(bodyLimit)
	gateway.Use(clientRequestID)
	gateway.Use(opsErrorLogger)
	gateway.Use(endpointNorm)
	gateway.Use(gin.HandlerFunc(apiKeyAuth))
	gateway.Use(requireGroupAnthropic)
	{
		// /v1/messages: auto-route based on group platform
		gateway.POST("/messages", func(c *gin.Context) {
			switch getGroupPlatform(c) {
			case service.PlatformOpenAI:
				h.OpenAIGateway.Messages(c)
				return
			case service.PlatformMiniMax:
				if h.MiniMaxGateway == nil {
					c.JSON(http.StatusServiceUnavailable, gin.H{
						"type": "error",
						"error": gin.H{
							"type":    "api_error",
							"message": "minimax gateway service unavailable",
						},
					})
					return
				}
				h.MiniMaxGateway.Messages(c)
				return
			case service.PlatformGLM:
				if h.GLMGateway == nil {
					c.JSON(http.StatusServiceUnavailable, gin.H{
						"type": "error",
						"error": gin.H{
							"type":    "api_error",
							"message": "glm gateway service unavailable",
						},
					})
					return
				}
				h.GLMGateway.Messages(c)
				return
			case service.PlatformKimi:
				if h.KimiGateway == nil {
					c.JSON(http.StatusServiceUnavailable, gin.H{
						"type": "error",
						"error": gin.H{
							"type":    "api_error",
							"message": "kimi gateway service unavailable",
						},
					})
					return
				}
				h.KimiGateway.Messages(c)
				return
			case service.PlatformDeepSeek:
				if h.DeepSeekGateway == nil {
					c.JSON(http.StatusServiceUnavailable, gin.H{
						"type": "error",
						"error": gin.H{
							"type":    "api_error",
							"message": "deepseek gateway service unavailable",
						},
					})
					return
				}
				h.DeepSeekGateway.Messages(c)
				return
			case service.PlatformWindsurf:
				if h.WindsurfGateway == nil {
					c.JSON(http.StatusServiceUnavailable, gin.H{
						"type": "error",
						"error": gin.H{
							"type":    "api_error",
							"message": "windsurf gateway service unavailable",
						},
					})
					return
				}
				h.WindsurfGateway.Messages(c)
				return
			}
			h.Gateway.Messages(c)
		})
		// /v1/messages/count_tokens: OpenAI groups get 404
		gateway.POST("/messages/count_tokens", func(c *gin.Context) {
			if getGroupPlatform(c) == service.PlatformOpenAI {
				c.JSON(http.StatusNotFound, gin.H{
					"type": "error",
					"error": gin.H{
						"type":    "not_found_error",
						"message": "Token counting is not supported for this platform",
					},
				})
				return
			}
			if getGroupPlatform(c) == service.PlatformMiniMax {
				writeMiniMaxUnsupported(c, h)
				return
			}
			if getGroupPlatform(c) == service.PlatformGLM {
				writeGLMUnsupported(c, h)
				return
			}
			if getGroupPlatform(c) == service.PlatformKimi {
				writeKimiUnsupported(c, h)
				return
			}
			if getGroupPlatform(c) == service.PlatformDeepSeek {
				writeDeepSeekUnsupported(c, h)
				return
			}
			if getGroupPlatform(c) == service.PlatformWindsurf {
				writeWindsurfUnsupported(c, h)
				return
			}
			h.Gateway.CountTokens(c)
		})
		gateway.GET("/models", func(c *gin.Context) {
			h.Gateway.Models(c)
		})
		gateway.GET("/usage", func(c *gin.Context) {
			if getGroupPlatform(c) == service.PlatformMiniMax {
				writeMiniMaxUnsupported(c, h)
				return
			}
			if getGroupPlatform(c) == service.PlatformGLM {
				writeGLMUnsupported(c, h)
				return
			}
			if getGroupPlatform(c) == service.PlatformKimi {
				writeKimiUnsupported(c, h)
				return
			}
			if getGroupPlatform(c) == service.PlatformDeepSeek {
				writeDeepSeekUnsupported(c, h)
				return
			}
			if getGroupPlatform(c) == service.PlatformWindsurf {
				writeWindsurfUnsupported(c, h)
				return
			}
			h.Gateway.Usage(c)
		})
		// OpenAI Responses API: auto-route based on group platform
		gateway.POST("/responses", func(c *gin.Context) {
			switch getGroupPlatform(c) {
			case service.PlatformOpenAI:
				h.OpenAIGateway.Responses(c)
				return
			case service.PlatformMiniMax:
				writeMiniMaxUnsupported(c, h)
				return
			case service.PlatformGLM:
				writeGLMUnsupported(c, h)
				return
			case service.PlatformKimi:
				writeKimiUnsupported(c, h)
				return
			case service.PlatformDeepSeek:
				writeDeepSeekUnsupported(c, h)
				return
			case service.PlatformWindsurf:
				writeWindsurfUnsupported(c, h)
				return
			}
			h.Gateway.Responses(c)
		})
		gateway.POST("/responses/*subpath", func(c *gin.Context) {
			switch getGroupPlatform(c) {
			case service.PlatformOpenAI:
				h.OpenAIGateway.Responses(c)
				return
			case service.PlatformMiniMax:
				writeMiniMaxUnsupported(c, h)
				return
			case service.PlatformGLM:
				writeGLMUnsupported(c, h)
				return
			case service.PlatformKimi:
				writeKimiUnsupported(c, h)
				return
			case service.PlatformDeepSeek:
				writeDeepSeekUnsupported(c, h)
				return
			case service.PlatformWindsurf:
				writeWindsurfUnsupported(c, h)
				return
			}
			h.Gateway.Responses(c)
		})
		gateway.GET("/responses", func(c *gin.Context) {
			if getGroupPlatform(c) == service.PlatformMiniMax {
				writeMiniMaxUnsupported(c, h)
				return
			}
			if getGroupPlatform(c) == service.PlatformGLM {
				writeGLMUnsupported(c, h)
				return
			}
			if getGroupPlatform(c) == service.PlatformKimi {
				writeKimiUnsupported(c, h)
				return
			}
			if getGroupPlatform(c) == service.PlatformDeepSeek {
				writeDeepSeekUnsupported(c, h)
				return
			}
			if getGroupPlatform(c) == service.PlatformWindsurf {
				writeWindsurfUnsupported(c, h)
				return
			}
			h.OpenAIGateway.ResponsesWebSocket(c)
		})
		// OpenAI Chat Completions API: auto-route based on group platform
		gateway.POST("/chat/completions", func(c *gin.Context) {
			switch getGroupPlatform(c) {
			case service.PlatformOpenAI:
				h.OpenAIGateway.ChatCompletions(c)
				return
			case service.PlatformMiniMax:
				writeMiniMaxChatCompletions(c, h)
				return
			case service.PlatformGLM:
				writeGLMChatCompletions(c, h)
				return
			case service.PlatformKimi:
				writeKimiChatCompletions(c, h)
				return
			case service.PlatformDeepSeek:
				writeDeepSeekChatCompletions(c, h)
				return
			case service.PlatformWindsurf:
				writeWindsurfChatCompletions(c, h)
				return
			}
			h.Gateway.ChatCompletions(c)
		})
		gateway.POST("/images/generations", func(c *gin.Context) {
			if getGroupPlatform(c) == service.PlatformMiniMax {
				writeMiniMaxUnsupported(c, h)
				return
			}
			if getGroupPlatform(c) == service.PlatformGLM {
				writeGLMUnsupported(c, h)
				return
			}
			if getGroupPlatform(c) == service.PlatformKimi {
				writeKimiUnsupported(c, h)
				return
			}
			if getGroupPlatform(c) == service.PlatformDeepSeek {
				writeDeepSeekUnsupported(c, h)
				return
			}
			if getGroupPlatform(c) == service.PlatformWindsurf {
				writeWindsurfUnsupported(c, h)
				return
			}
			if getGroupPlatform(c) != service.PlatformOpenAI {
				c.JSON(http.StatusNotFound, gin.H{
					"error": gin.H{
						"type":    "not_found_error",
						"message": "Images API is not supported for this platform",
					},
				})
				return
			}
			h.OpenAIGateway.Images(c)
		})
		gateway.POST("/images/edits", func(c *gin.Context) {
			if getGroupPlatform(c) == service.PlatformMiniMax {
				writeMiniMaxUnsupported(c, h)
				return
			}
			if getGroupPlatform(c) == service.PlatformGLM {
				writeGLMUnsupported(c, h)
				return
			}
			if getGroupPlatform(c) == service.PlatformKimi {
				writeKimiUnsupported(c, h)
				return
			}
			if getGroupPlatform(c) == service.PlatformDeepSeek {
				writeDeepSeekUnsupported(c, h)
				return
			}
			if getGroupPlatform(c) == service.PlatformWindsurf {
				writeWindsurfUnsupported(c, h)
				return
			}
			if getGroupPlatform(c) != service.PlatformOpenAI {
				c.JSON(http.StatusNotFound, gin.H{
					"error": gin.H{
						"type":    "not_found_error",
						"message": "Images API is not supported for this platform",
					},
				})
				return
			}
			h.OpenAIGateway.Images(c)
		})
	}

	// Gemini 原生 API 兼容层（Gemini SDK/CLI 直连）
	gemini := r.Group("/v1beta")
	gemini.Use(bodyLimit)
	gemini.Use(clientRequestID)
	gemini.Use(opsErrorLogger)
	gemini.Use(endpointNorm)
	gemini.Use(middleware.APIKeyAuthWithSubscriptionGoogle(apiKeyService, subscriptionService, cfg))
	gemini.Use(requireGroupGoogle)
	{
		gemini.GET("/models", h.Gateway.GeminiV1BetaListModels)
		gemini.GET("/models/:model", h.Gateway.GeminiV1BetaGetModel)
		// Gin treats ":" as a param marker, but Gemini uses "{model}:{action}" in the same segment.
		gemini.POST("/models/*modelAction", h.Gateway.GeminiV1BetaModels)
	}

	// OpenAI Responses API（不带v1前缀的别名）— auto-route based on group platform
	responsesHandler := func(c *gin.Context) {
		switch getGroupPlatform(c) {
		case service.PlatformOpenAI:
			h.OpenAIGateway.Responses(c)
			return
		case service.PlatformMiniMax:
			writeMiniMaxUnsupported(c, h)
			return
		case service.PlatformGLM:
			writeGLMUnsupported(c, h)
			return
		case service.PlatformKimi:
			writeKimiUnsupported(c, h)
			return
		case service.PlatformDeepSeek:
			writeDeepSeekUnsupported(c, h)
			return
		case service.PlatformWindsurf:
			writeWindsurfUnsupported(c, h)
			return
		}
		h.Gateway.Responses(c)
	}
	r.POST("/responses", bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), requireGroupAnthropic, responsesHandler)
	r.POST("/responses/*subpath", bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), requireGroupAnthropic, responsesHandler)
	r.GET("/responses", bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), requireGroupAnthropic, func(c *gin.Context) {
		if getGroupPlatform(c) == service.PlatformMiniMax {
			writeMiniMaxUnsupported(c, h)
			return
		}
		if getGroupPlatform(c) == service.PlatformGLM {
			writeGLMUnsupported(c, h)
			return
		}
		if getGroupPlatform(c) == service.PlatformKimi {
			writeKimiUnsupported(c, h)
			return
		}
		if getGroupPlatform(c) == service.PlatformDeepSeek {
			writeDeepSeekUnsupported(c, h)
			return
		}
		if getGroupPlatform(c) == service.PlatformWindsurf {
			writeWindsurfUnsupported(c, h)
			return
		}
		h.OpenAIGateway.ResponsesWebSocket(c)
	})
	codexDirect := r.Group("/backend-api/codex")
	codexDirect.Use(bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), requireGroupAnthropic)
	{
		codexDirect.POST("/responses", responsesHandler)
		codexDirect.POST("/responses/*subpath", responsesHandler)
		codexDirect.GET("/responses", func(c *gin.Context) {
			if getGroupPlatform(c) == service.PlatformMiniMax {
				writeMiniMaxUnsupported(c, h)
				return
			}
			if getGroupPlatform(c) == service.PlatformGLM {
				writeGLMUnsupported(c, h)
				return
			}
			if getGroupPlatform(c) == service.PlatformKimi {
				writeKimiUnsupported(c, h)
				return
			}
			if getGroupPlatform(c) == service.PlatformDeepSeek {
				writeDeepSeekUnsupported(c, h)
				return
			}
			if getGroupPlatform(c) == service.PlatformWindsurf {
				writeWindsurfUnsupported(c, h)
				return
			}
			h.OpenAIGateway.ResponsesWebSocket(c)
		})
	}
	// OpenAI Chat Completions API（不带v1前缀的别名）— auto-route based on group platform
	r.POST("/chat/completions", bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), requireGroupAnthropic, func(c *gin.Context) {
		switch getGroupPlatform(c) {
		case service.PlatformOpenAI:
			h.OpenAIGateway.ChatCompletions(c)
			return
		case service.PlatformMiniMax:
			writeMiniMaxChatCompletions(c, h)
			return
		case service.PlatformGLM:
			writeGLMChatCompletions(c, h)
			return
		case service.PlatformKimi:
			writeKimiChatCompletions(c, h)
			return
		case service.PlatformDeepSeek:
			writeDeepSeekChatCompletions(c, h)
			return
		case service.PlatformWindsurf:
			writeWindsurfChatCompletions(c, h)
			return
		}
		h.Gateway.ChatCompletions(c)
	})
	r.POST("/images/generations", bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), requireGroupAnthropic, func(c *gin.Context) {
		if getGroupPlatform(c) == service.PlatformMiniMax {
			writeMiniMaxUnsupported(c, h)
			return
		}
		if getGroupPlatform(c) == service.PlatformGLM {
			writeGLMUnsupported(c, h)
			return
		}
		if getGroupPlatform(c) == service.PlatformKimi {
			writeKimiUnsupported(c, h)
			return
		}
		if getGroupPlatform(c) == service.PlatformDeepSeek {
			writeDeepSeekUnsupported(c, h)
			return
		}
		if getGroupPlatform(c) == service.PlatformWindsurf {
			writeWindsurfUnsupported(c, h)
			return
		}
		if getGroupPlatform(c) != service.PlatformOpenAI {
			c.JSON(http.StatusNotFound, gin.H{
				"error": gin.H{
					"type":    "not_found_error",
					"message": "Images API is not supported for this platform",
				},
			})
			return
		}
		h.OpenAIGateway.Images(c)
	})
	r.POST("/images/edits", bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), requireGroupAnthropic, func(c *gin.Context) {
		if getGroupPlatform(c) == service.PlatformMiniMax {
			writeMiniMaxUnsupported(c, h)
			return
		}
		if getGroupPlatform(c) == service.PlatformGLM {
			writeGLMUnsupported(c, h)
			return
		}
		if getGroupPlatform(c) == service.PlatformKimi {
			writeKimiUnsupported(c, h)
			return
		}
		if getGroupPlatform(c) == service.PlatformDeepSeek {
			writeDeepSeekUnsupported(c, h)
			return
		}
		if getGroupPlatform(c) == service.PlatformWindsurf {
			writeWindsurfUnsupported(c, h)
			return
		}
		if getGroupPlatform(c) != service.PlatformOpenAI {
			c.JSON(http.StatusNotFound, gin.H{
				"error": gin.H{
					"type":    "not_found_error",
					"message": "Images API is not supported for this platform",
				},
			})
			return
		}
		h.OpenAIGateway.Images(c)
	})

	// Antigravity 模型列表
	r.GET("/antigravity/models", gin.HandlerFunc(apiKeyAuth), requireGroupAnthropic, h.Gateway.AntigravityModels)

	// Antigravity 专用路由（仅使用 antigravity 账户，不混合调度）
	antigravityV1 := r.Group("/antigravity/v1")
	antigravityV1.Use(bodyLimit)
	antigravityV1.Use(clientRequestID)
	antigravityV1.Use(opsErrorLogger)
	antigravityV1.Use(endpointNorm)
	antigravityV1.Use(middleware.ForcePlatform(service.PlatformAntigravity))
	antigravityV1.Use(gin.HandlerFunc(apiKeyAuth))
	antigravityV1.Use(requireGroupAnthropic)
	{
		antigravityV1.POST("/messages", h.Gateway.Messages)
		antigravityV1.POST("/messages/count_tokens", h.Gateway.CountTokens)
		antigravityV1.GET("/models", h.Gateway.AntigravityModels)
		antigravityV1.GET("/usage", h.Gateway.Usage)
	}

	antigravityV1Beta := r.Group("/antigravity/v1beta")
	antigravityV1Beta.Use(bodyLimit)
	antigravityV1Beta.Use(clientRequestID)
	antigravityV1Beta.Use(opsErrorLogger)
	antigravityV1Beta.Use(endpointNorm)
	antigravityV1Beta.Use(middleware.ForcePlatform(service.PlatformAntigravity))
	antigravityV1Beta.Use(middleware.APIKeyAuthWithSubscriptionGoogle(apiKeyService, subscriptionService, cfg))
	antigravityV1Beta.Use(requireGroupGoogle)
	{
		antigravityV1Beta.GET("/models", h.Gateway.GeminiV1BetaListModels)
		antigravityV1Beta.GET("/models/:model", h.Gateway.GeminiV1BetaGetModel)
		antigravityV1Beta.POST("/models/*modelAction", h.Gateway.GeminiV1BetaModels)
	}

}

// getGroupPlatform extracts the group platform from the API Key stored in context.
func getGroupPlatform(c *gin.Context) string {
	apiKey, ok := middleware.GetAPIKeyFromContext(c)
	if !ok || apiKey.Group == nil {
		return ""
	}
	return apiKey.Group.Platform
}

func writeMiniMaxUnsupported(c *gin.Context, h *handler.Handlers) {
	if h != nil && h.MiniMaxGateway != nil {
		h.MiniMaxGateway.Unsupported(c)
		return
	}
	c.JSON(http.StatusNotFound, gin.H{
		"type": "error",
		"error": gin.H{
			"type":    "not_found_error",
			"message": "MiniMax gateway supports /v1/messages, /v1/chat/completions, and /v1/models only",
		},
	})
}

func writeMiniMaxChatCompletions(c *gin.Context, h *handler.Handlers) {
	if h != nil && h.MiniMaxGateway != nil {
		h.MiniMaxGateway.ChatCompletions(c)
		return
	}
	c.JSON(http.StatusServiceUnavailable, gin.H{
		"type": "error",
		"error": gin.H{
			"type":    "api_error",
			"message": "minimax gateway service unavailable",
		},
	})
}

func writeGLMUnsupported(c *gin.Context, h *handler.Handlers) {
	if h != nil && h.GLMGateway != nil {
		h.GLMGateway.Unsupported(c)
		return
	}
	c.JSON(http.StatusNotFound, gin.H{
		"type": "error",
		"error": gin.H{
			"type":    "not_found_error",
			"message": "GLM gateway supports /v1/messages and /v1/chat/completions only",
		},
	})
}

func writeGLMChatCompletions(c *gin.Context, h *handler.Handlers) {
	if h != nil && h.GLMGateway != nil {
		h.GLMGateway.ChatCompletions(c)
		return
	}
	c.JSON(http.StatusServiceUnavailable, gin.H{
		"type": "error",
		"error": gin.H{
			"type":    "api_error",
			"message": "glm gateway service unavailable",
		},
	})
}

func writeKimiUnsupported(c *gin.Context, h *handler.Handlers) {
	if h != nil && h.KimiGateway != nil {
		h.KimiGateway.Unsupported(c)
		return
	}
	c.JSON(http.StatusNotFound, gin.H{
		"type": "error",
		"error": gin.H{
			"type":    "not_found_error",
			"message": "Kimi gateway supports /v1/messages and /v1/chat/completions only",
		},
	})
}

func writeKimiChatCompletions(c *gin.Context, h *handler.Handlers) {
	if h != nil && h.KimiGateway != nil {
		h.KimiGateway.ChatCompletions(c)
		return
	}
	c.JSON(http.StatusServiceUnavailable, gin.H{
		"type": "error",
		"error": gin.H{
			"type":    "api_error",
			"message": "kimi gateway service unavailable",
		},
	})
}

func writeDeepSeekUnsupported(c *gin.Context, h *handler.Handlers) {
	if h != nil && h.DeepSeekGateway != nil {
		h.DeepSeekGateway.Unsupported(c)
		return
	}
	c.JSON(http.StatusNotFound, gin.H{
		"type": "error",
		"error": gin.H{
			"type":    "not_found_error",
			"message": "DeepSeek gateway supports /v1/messages and /v1/chat/completions only",
		},
	})
}

func writeDeepSeekChatCompletions(c *gin.Context, h *handler.Handlers) {
	if h != nil && h.DeepSeekGateway != nil {
		h.DeepSeekGateway.ChatCompletions(c)
		return
	}
	c.JSON(http.StatusServiceUnavailable, gin.H{
		"type": "error",
		"error": gin.H{
			"type":    "api_error",
			"message": "deepseek gateway service unavailable",
		},
	})
}

func writeWindsurfUnsupported(c *gin.Context, h *handler.Handlers) {
	if h != nil && h.WindsurfGateway != nil {
		h.WindsurfGateway.Unsupported(c)
		return
	}
	c.JSON(http.StatusNotFound, gin.H{
		"type": "error",
		"error": gin.H{
			"type":    "not_found_error",
			"message": "Windsurf gateway supports /v1/messages and /v1/chat/completions only",
		},
	})
}

func writeWindsurfChatCompletions(c *gin.Context, h *handler.Handlers) {
	if h != nil && h.WindsurfGateway != nil {
		h.WindsurfGateway.ChatCompletions(c)
		return
	}
	c.JSON(http.StatusServiceUnavailable, gin.H{
		"type": "error",
		"error": gin.H{
			"type":    "api_error",
			"message": "windsurf gateway service unavailable",
		},
	})
}
