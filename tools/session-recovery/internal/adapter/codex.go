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

// CodexAdapter Codex 平台适配器
type CodexAdapter struct {
	sessionPath string
	configPath  string
	jsonParser  *parser.JSONParser
	jsonlParser *parser.JSONLParser
}

// NewCodexAdapter 创建 Codex 适配器
func NewCodexAdapter() *CodexAdapter {
	homeDir, _ := os.UserHomeDir()
	return &CodexAdapter{
		sessionPath: filepath.Join(homeDir, ".codex", "sessions"),
		configPath:  filepath.Join(homeDir, ".codex", "config.json"),
		jsonParser:  parser.NewJSONParser(),
		jsonlParser: parser.NewJSONLParser(),
	}
}

func (c *CodexAdapter) Name() string {
	return "codex"
}

func (c *CodexAdapter) DisplayName() string {
	return "Codex"
}

func (c *CodexAdapter) DefaultSessionPath() string {
	return c.sessionPath
}

func (c *CodexAdapter) IsInstalled() bool {
	_, err := os.Stat(c.sessionPath)
	return err == nil
}

func (c *CodexAdapter) GetCurrentProvider() (string, error) {
	// 读取配置文件获取当前 Provider
	data, err := os.ReadFile(c.configPath)
	if err != nil {
		return "", fmt.Errorf("read config failed: %w", err)
	}

	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		return "", fmt.Errorf("parse config failed: %w", err)
	}

	// 尝试多种可能的字段名
	if provider, ok := config["provider"].(string); ok {
		return c.normalizeProvider(provider), nil
	}
	if provider, ok := config["ai_provider"].(string); ok {
		return c.normalizeProvider(provider), nil
	}
	if provider, ok := config["llm_provider"].(string); ok {
		return c.normalizeProvider(provider), nil
	}

	return "Unknown", nil
}

func (c *CodexAdapter) GetCurrentAPIKey() (string, error) {
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
	if apiKey, ok := config["apikey"].(string); ok {
		return apiKey, nil
	}

	return "", fmt.Errorf("api_key not found in config")
}

func (c *CodexAdapter) ScanAllSessions(ctx context.Context) ([]*model.Session, error) {
	var sessions []*model.Session

	// 遍历会话目录
	err := filepath.Walk(c.sessionPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // 跳过无法访问的文件
		}

		if info.IsDir() {
			return nil
		}

		// 只处理 JSON 和 JSONL 文件
		ext := filepath.Ext(path)
		if ext != ".json" && ext != ".jsonl" {
			return nil
		}

		// 解析会话文件
		session, err := c.ParseSessionFile(ctx, path)
		if err != nil {
			// 跳过解析失败的文件
			return nil
		}

		session.Platform = "codex"
		sessions = append(sessions, session)
		return nil
	})

	if err != nil {
		return nil, err
	}

	return sessions, nil
}

func (c *CodexAdapter) ParseSessionFile(ctx context.Context, path string) (*model.Session, error) {
	ext := filepath.Ext(path)

	var session *model.Session
	var err error

	if ext == ".jsonl" {
		session, err = c.jsonlParser.ParseFile(ctx, path)
	} else {
		session, err = c.jsonParser.ParseFile(ctx, path)
	}

	if err != nil {
		return nil, err
	}

	// 如果没有 ID，使用文件名作为 ID
	if session.ID == "" {
		session.ID = filepath.Base(path)
		session.ID = session.ID[:len(session.ID)-len(ext)]
	}

	return session, nil
}

func (c *CodexAdapter) GetSessionMetadata(session *model.Session) (*model.Metadata, error) {
	data, err := os.ReadFile(session.FilePath)
	if err != nil {
		return nil, err
	}

	ext := filepath.Ext(session.FilePath)
	if ext == ".jsonl" {
		// JSONL 文件需要逐行读取
		return nil, fmt.Errorf("JSONL metadata extraction not implemented yet")
	}

	var sessionData map[string]interface{}
	if err := json.Unmarshal(data, &sessionData); err != nil {
		return nil, err
	}

	meta := &model.Metadata{
		SessionID: session.ID,
		Custom:    make(map[string]interface{}),
	}

	if provider, ok := sessionData["provider"].(string); ok {
		meta.Provider = provider
	}
	if apiKey, ok := sessionData["api_key"].(string); ok {
		meta.APIKey = apiKey
	}
	if userID, ok := sessionData["user_id"].(string); ok {
		meta.UserID = userID
	}

	return meta, nil
}

func (c *CodexAdapter) UpdateSessionMetadata(session *model.Session, meta *model.Metadata) error {
	data, err := os.ReadFile(session.FilePath)
	if err != nil {
		return err
	}

	ext := filepath.Ext(session.FilePath)
	if ext == ".jsonl" {
		// JSONL 文件需要逐行处理
		return c.updateJSONLMetadata(session.FilePath, meta)
	}

	// JSON 文件直接更新
	var sessionData map[string]interface{}
	if err := json.Unmarshal(data, &sessionData); err != nil {
		return err
	}

	// 更新元数据
	sessionData["provider"] = meta.Provider
	sessionData["api_key"] = meta.APIKey
	if meta.UserID != "" {
		sessionData["user_id"] = meta.UserID
	}

	// 写回文件
	updated, err := json.MarshalIndent(sessionData, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(session.FilePath, updated, 0644)
}

func (c *CodexAdapter) updateJSONLMetadata(filePath string, meta *model.Metadata) error {
	// TODO: 实现 JSONL 文件的元数据更新
	return fmt.Errorf("JSONL metadata update not implemented yet")
}

func (c *CodexAdapter) RestoreSession(ctx context.Context, session *model.Session) error {
	// Codex 的恢复逻辑：更新元数据即可
	currentProvider, err := c.GetCurrentProvider()
	if err != nil {
		return err
	}

	currentAPIKey, err := c.GetCurrentAPIKey()
	if err != nil {
		return err
	}

	meta := &model.Metadata{
		Provider:  currentProvider,
		APIKey:    currentAPIKey,
		SessionID: session.ID,
	}

	return c.UpdateSessionMetadata(session, meta)
}

func (c *CodexAdapter) VerifyRestored(ctx context.Context, session *model.Session) (bool, error) {
	// 验证：重新读取文件，检查元数据是否正确
	updatedMeta, err := c.GetSessionMetadata(session)
	if err != nil {
		return false, err
	}

	currentProvider, _ := c.GetCurrentProvider()
	if updatedMeta.Provider != currentProvider {
		return false, fmt.Errorf("provider mismatch")
	}

	return true, nil
}

// normalizeProvider 规范化 Provider 名称
func (c *CodexAdapter) normalizeProvider(provider string) string {
	providerMap := map[string]string{
		"anthropic": "Anthropic",
		"openai":    "OpenAI",
		"gemini":    "Gemini",
		"claude":    "Anthropic",
		"gpt":       "OpenAI",
	}

	if normalized, ok := providerMap[provider]; ok {
		return normalized
	}

	// 首字母大写
	if len(provider) > 0 {
		return string(provider[0]-32) + provider[1:]
	}

	return provider
}
