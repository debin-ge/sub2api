package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sub2api/session-recovery/internal/model"
	"github.com/sub2api/session-recovery/internal/parser"
)

// ClaudeCodeAdapter Claude Code 平台适配器
type ClaudeCodeAdapter struct {
	sessionPath string
	configPath  string
	jsonlParser *parser.JSONLParser
}

// NewClaudeCodeAdapter 创建 Claude Code 适配器
func NewClaudeCodeAdapter() *ClaudeCodeAdapter {
	homeDir, _ := os.UserHomeDir()
	return &ClaudeCodeAdapter{
		sessionPath: filepath.Join(homeDir, ".claude", "projects"),
		configPath:  filepath.Join(homeDir, ".claude", "config.json"),
		jsonlParser: parser.NewJSONLParser(),
	}
}

func (c *ClaudeCodeAdapter) Name() string {
	return "claude_code"
}

func (c *ClaudeCodeAdapter) DisplayName() string {
	return "Claude Code"
}

func (c *ClaudeCodeAdapter) DefaultSessionPath() string {
	return c.sessionPath
}

func (c *ClaudeCodeAdapter) IsInstalled() bool {
	_, err := os.Stat(c.sessionPath)
	return err == nil
}

func (c *ClaudeCodeAdapter) GetCurrentProvider() (string, error) {
	// Claude Code 通常使用 Anthropic
	return "Anthropic", nil
}

func (c *ClaudeCodeAdapter) GetCurrentAPIKey() (string, error) {
	// Claude Code 的 API Key 通常存储在配置中
	data, err := os.ReadFile(c.configPath)
	if err != nil {
		return "", fmt.Errorf("read config failed: %w", err)
	}

	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		return "", fmt.Errorf("parse config failed: %w", err)
	}

	if apiKey, ok := config["api_key"].(string); ok {
		return apiKey, nil
	}

	return "", fmt.Errorf("api_key not found in config")
}

func (c *ClaudeCodeAdapter) ScanAllSessions(ctx context.Context) ([]*model.Session, error) {
	var sessions []*model.Session

	// 遍历项目目录
	projects, err := os.ReadDir(c.sessionPath)
	if err != nil {
		return nil, err
	}

	for _, projectDir := range projects {
		if !projectDir.IsDir() {
			continue
		}

		projectPath := filepath.Join(c.sessionPath, projectDir.Name())

		// Claude Code 的会话文件直接在项目目录下，不在 sessions/ 子目录
		// 遍历项目目录下的所有 JSONL 文件
		files, err := os.ReadDir(projectPath)
		if err != nil {
			continue
		}

		for _, file := range files {
			if file.IsDir() {
				continue
			}

			// 只处理 JSONL 文件
			if filepath.Ext(file.Name()) != ".jsonl" {
				continue
			}

			filePath := filepath.Join(projectPath, file.Name())

			// 解析会话文件
			session, err := c.ParseSessionFile(ctx, filePath)
			if err != nil {
				continue
			}

			session.Platform = "claude_code"
			session.Project = projectDir.Name()
			sessions = append(sessions, session)
		}
	}

	return sessions, nil
}

func (c *ClaudeCodeAdapter) ParseSessionFile(ctx context.Context, path string) (*model.Session, error) {
	session, err := c.jsonlParser.ParseFile(ctx, path)
	if err != nil {
		return nil, err
	}

	// 如果没有 ID，使用文件名作为 ID
	if session.ID == "" {
		session.ID = filepath.Base(path)
		ext := filepath.Ext(session.ID)
		session.ID = session.ID[:len(session.ID)-len(ext)]
	}

	return session, nil
}

func (c *ClaudeCodeAdapter) GetSessionMetadata(session *model.Session) (*model.Metadata, error) {
	// Claude Code 的会话元数据通常嵌入在 JSONL 中
	// 简化实现：返回基本元数据
	meta := &model.Metadata{
		SessionID: session.ID,
		Provider:  "Anthropic",
		Custom:    make(map[string]interface{}),
	}

	return meta, nil
}

func (c *ClaudeCodeAdapter) UpdateSessionMetadata(session *model.Session, meta *model.Metadata) error {
	// Claude Code 通常不需要修改元数据
	// 因为会话不按 Provider 隔离
	return nil
}

func (c *ClaudeCodeAdapter) RestoreSession(ctx context.Context, session *model.Session) error {
	// Claude Code 的恢复逻辑：通常不需要修改
	// 会话文件已经存在，只需确保在正确位置
	return nil
}

func (c *ClaudeCodeAdapter) VerifyRestored(ctx context.Context, session *model.Session) (bool, error) {
	// 验证：检查文件是否存在
	_, err := os.Stat(session.FilePath)
	if err != nil {
		return false, err
	}

	return true, nil
}
