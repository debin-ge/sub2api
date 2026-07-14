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

// GeminiAdapter Gemini CLI 平台适配器
type GeminiAdapter struct {
	sessionPath string
	configPath  string
	jsonParser  *parser.JSONParser
}

// NewGeminiAdapter 创建 Gemini 适配器
func NewGeminiAdapter() *GeminiAdapter {
	homeDir, _ := os.UserHomeDir()
	return &GeminiAdapter{
		sessionPath: filepath.Join(homeDir, ".gemini", "tmp"),
		configPath:  filepath.Join(homeDir, ".gemini", "config"),
		jsonParser:  parser.NewJSONParser(),
	}
}

func (g *GeminiAdapter) Name() string {
	return "gemini"
}

func (g *GeminiAdapter) DisplayName() string {
	return "Gemini CLI"
}

func (g *GeminiAdapter) DefaultSessionPath() string {
	return g.sessionPath
}

func (g *GeminiAdapter) IsInstalled() bool {
	_, err := os.Stat(g.sessionPath)
	return err == nil
}

func (g *GeminiAdapter) GetCurrentProvider() (string, error) {
	return "Google", nil
}

func (g *GeminiAdapter) GetCurrentAPIKey() (string, error) {
	configFile := filepath.Join(g.configPath, "api_key")
	data, err := os.ReadFile(configFile)
	if err != nil {
		return "", fmt.Errorf("read api key failed: %w", err)
	}

	return string(data), nil
}

func (g *GeminiAdapter) ScanAllSessions(ctx context.Context) ([]*model.Session, error) {
	var sessions []*model.Session

	err := filepath.Walk(g.sessionPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		if info.IsDir() {
			return nil
		}

		if filepath.Ext(path) != ".json" {
			return nil
		}

		session, err := g.ParseSessionFile(ctx, path)
		if err != nil {
			return nil
		}

		session.Platform = "gemini"
		sessions = append(sessions, session)
		return nil
	})

	if err != nil {
		return nil, err
	}

	return sessions, nil
}

func (g *GeminiAdapter) ParseSessionFile(ctx context.Context, path string) (*model.Session, error) {
	session, err := g.jsonParser.ParseFile(ctx, path)
	if err != nil {
		return nil, err
	}

	if session.ID == "" {
		session.ID = filepath.Base(path)
		ext := filepath.Ext(session.ID)
		session.ID = session.ID[:len(session.ID)-len(ext)]
	}

	return session, nil
}

func (g *GeminiAdapter) GetSessionMetadata(session *model.Session) (*model.Metadata, error) {
	data, err := os.ReadFile(session.FilePath)
	if err != nil {
		return nil, err
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

	return meta, nil
}

func (g *GeminiAdapter) UpdateSessionMetadata(session *model.Session, meta *model.Metadata) error {
	data, err := os.ReadFile(session.FilePath)
	if err != nil {
		return err
	}

	var sessionData map[string]interface{}
	if err := json.Unmarshal(data, &sessionData); err != nil {
		return err
	}

	sessionData["provider"] = meta.Provider

	updated, err := json.MarshalIndent(sessionData, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(session.FilePath, updated, 0644)
}

func (g *GeminiAdapter) RestoreSession(ctx context.Context, session *model.Session) error {
	currentProvider, err := g.GetCurrentProvider()
	if err != nil {
		return err
	}

	meta := &model.Metadata{
		Provider:  currentProvider,
		SessionID: session.ID,
	}

	return g.UpdateSessionMetadata(session, meta)
}

func (g *GeminiAdapter) VerifyRestored(ctx context.Context, session *model.Session) (bool, error) {
	_, err := os.Stat(session.FilePath)
	if err != nil {
		return false, err
	}

	return true, nil
}
