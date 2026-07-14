package restorer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/sub2api/session-recovery/internal/adapter"
	"github.com/sub2api/session-recovery/internal/model"
)

// Restorer 会话恢复器
type Restorer struct {
	adapter    adapter.Adapter
	backupPath string
}

// NewRestorer 创建会话恢复器
func NewRestorer(adapter adapter.Adapter) *Restorer {
	homeDir, _ := os.UserHomeDir()
	backupPath := filepath.Join(homeDir, ".session-recovery", "backups")

	return &Restorer{
		adapter:    adapter,
		backupPath: backupPath,
	}
}

// RestoreSession 恢复单个会话
func (r *Restorer) RestoreSession(ctx context.Context, session *model.Session) error {
	// 1. 备份原始文件
	if err := r.backupSession(session); err != nil {
		return fmt.Errorf("backup failed: %w", err)
	}

	// 2. 获取当前配置
	currentProvider, err := r.adapter.GetCurrentProvider()
	if err != nil {
		return fmt.Errorf("get current provider failed: %w", err)
	}

	currentAPIKey, err := r.adapter.GetCurrentAPIKey()
	if err != nil {
		return fmt.Errorf("get current api key failed: %w", err)
	}

	// 3. 更新元数据
	meta := &model.Metadata{
		Provider:  currentProvider,
		APIKey:    currentAPIKey,
		SessionID: session.ID,
	}

	if err := r.adapter.UpdateSessionMetadata(session, meta); err != nil {
		r.rollback(session)
		return fmt.Errorf("update metadata failed: %w", err)
	}

	// 4. 验证恢复
	ok, err := r.adapter.VerifyRestored(ctx, session)
	if err != nil || !ok {
		r.rollback(session)
		return fmt.Errorf("verify failed: %w", err)
	}

	return nil
}

// backupSession 备份会话文件
func (r *Restorer) backupSession(session *model.Session) error {
	// 创建备份目录
	if err := os.MkdirAll(r.backupPath, 0755); err != nil {
		return err
	}

	// 生成备份文件名
	timestamp := time.Now().Format("20060102_150405")
	backupFileName := fmt.Sprintf("%s_%s.backup", session.ID, timestamp)
	backupFilePath := filepath.Join(r.backupPath, backupFileName)

	// 复制文件
	input, err := os.ReadFile(session.FilePath)
	if err != nil {
		return err
	}

	return os.WriteFile(backupFilePath, input, 0644)
}

// rollback 回滚操作
func (r *Restorer) rollback(session *model.Session) error {
	// 查找最新的备份文件
	// 简化实现：这里应该找到对应的备份并恢复
	return nil
}
