package model

import "time"

// Session 统一会话数据模型
type Session struct {
	// 基本信息
	ID       string `json:"id"`
	Platform string `json:"platform"` // 平台名称
	Project  string `json:"project"`  // 项目名称
	Branch   string `json:"branch"`   // 分支名称

	// 消息信息
	FirstMessage string `json:"first_message"` // 首条用户消息（用于展示）
	MessageCount int    `json:"message_count"` // 消息数量

	// 时间信息
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// 文件信息
	FilePath string `json:"file_path"` // 原始文件路径

	// Provider 信息（用于展示）
	OriginalProvider string `json:"original_provider"` // 原始 Provider
	CurrentProvider  string `json:"current_provider"`  // 当前 Provider

	// 内部使用（不展示）
	OriginalAPIKey string `json:"-"` // 原始 API Key
	CurrentAPIKey  string `json:"-"` // 当前 API Key

	// 状态标识
	IsVisible     bool `json:"is_visible"`      // 是否在平台中可见
	NeedsRecovery bool `json:"needs_recovery"`  // 是否需要恢复
}

// Metadata 会话元数据
type Metadata struct {
	Provider  string                 `json:"provider"`
	APIKey    string                 `json:"api_key"`
	UserID    string                 `json:"user_id,omitempty"`
	SessionID string                 `json:"session_id"`
	Custom    map[string]interface{} `json:"custom,omitempty"` // 平台特定字段
}
