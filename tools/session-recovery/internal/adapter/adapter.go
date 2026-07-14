package adapter

import (
	"context"

	"github.com/sub2api/session-recovery/internal/model"
)

// Adapter 平台适配器接口
type Adapter interface {
	// 平台基本信息
	Name() string        // 平台名称（如 "codex"）
	DisplayName() string // 显示名称（如 "Codex"）

	// 路径和配置
	DefaultSessionPath() string // 默认会话存储路径
	IsInstalled() bool          // 检测平台是否已安装

	// Provider 和配置
	GetCurrentProvider() (string, error) // 获取当前 Provider（如 "Anthropic"）
	GetCurrentAPIKey() (string, error)   // 获取当前 API Key（用于识别，不展示）

	// 会话扫描
	ScanAllSessions(ctx context.Context) ([]*model.Session, error)                      // 扫描所有会话
	ParseSessionFile(ctx context.Context, path string) (*model.Session, error)          // 解析单个会话

	// 会话恢复
	GetSessionMetadata(session *model.Session) (*model.Metadata, error)                 // 获取元数据
	UpdateSessionMetadata(session *model.Session, meta *model.Metadata) error           // 更新元数据
	RestoreSession(ctx context.Context, session *model.Session) error                   // 执行恢复

	// 验证
	VerifyRestored(ctx context.Context, session *model.Session) (bool, error) // 验证恢复成功
}
