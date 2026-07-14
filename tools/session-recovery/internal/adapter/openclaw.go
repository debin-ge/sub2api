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

// OpenClawAdapter OpenClaw 平台适配器
type OpenClawAdapter struct {
	sessionPath string
	configPath  string
	jsonParser  *parser.JSONParser
}

// NewOpenClawAdapter 创建 OpenClaw 适配器
func NewOpenClawAdapter() *OpenClawAdapter {
	homeDir, _ := os.UserHomeDir()
	return &OpenClawAdapter{
		sessionPath: filepath.Join(homeDir, ".openclaw", "agents", "main", "sessions"),
		configPath:  filepath.Join(homeDir, ".openclaw", "config.json"),
		jsonParser:  parser.NewJSONParser(),
	}
}

func (o *OpenClawAdapter) Name() string {
	return "openclaw"
}

func (o *OpenClawAdapter) DisplayName() string {
	return "OpenClaw"
}

func (o *OpenClawAdapter) DefaultSessionPath() string {
	return o.sessionPath
}

func (o *OpenClawAdapter) IsInstalled() bool {
	_, err := os.Stat(o.sessionPath)
	return err == nil
}

func (o *OpenClawAdapter) GetCurrentProvider() (string, error) {
	data, err := os.ReadFile(o.configPath)
	if err != nil {
		return "", fmt.Errorf("read config failed: %w", err)
	}

	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		return "", fmt.Errorf("parse config failed: %w", err)
	}

	if provider, ok := config["provider"].(string); ok {
		return provider, nil
	}

	return "Unknown", nil
}

func (o *OpenClawAdapter) GetCurrentAPIKey() (string, error) {
	data, err := os.ReadFile(o.configPath)
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

	return "", fmt.Errorf("api_key not found")
}

func (o *OpenClawAdapter) ScanAllSessions(ctx context.Context) ([]*model.Session, error) {
	var sessions []*model.Session

	err := filepath.Walk(o.sessionPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		if info.IsDir() {
			return nil
		}

		if filepath.Ext(path) != ".json" {
			return nil
		}

		session, err := o.ParseSessionFile(ctx, path)
		if err != nil {
			return nil
		}

		session.Platform = "openclaw"
		sessions = append(sessions, session)
		return nil
	})

	if err != nil {
		return nil, err
	}

	return sessions, nil
}

func (o *OpenClawAdapter) ParseSessionFile(ctx context.Context, path string) (*model.Session, error) {
	session, err := o.jsonParser.ParseFile(ctx, path)
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

func (o *OpenClawAdapter) GetSessionMetadata(session *model.Session) (*model.Metadata, error) {
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
	if apiKey, ok := sessionData["api_key"].(string); ok {
		meta.APIKey = apiKey
	}

	return meta, nil
}

func (o *OpenClawAdapter) UpdateSessionMetadata(session *model.Session, meta *model.Metadata) error {
	data, err := os.ReadFile(session.FilePath)
	if err != nil {
		return err
	}

	var sessionData map[string]interface{}
	if err := json.Unmarshal(data, &sessionData); err != nil {
		return err
	}

	sessionData["provider"] = meta.Provider
	sessionData["api_key"] = meta.APIKey

	updated, err := json.MarshalIndent(sessionData, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(session.FilePath, updated, 0644)
}

func (o *OpenClawAdapter) RestoreSession(ctx context.Context, session *model.Session) error {
	currentProvider, err := o.GetCurrentProvider()
	if err != nil {
		return err
	}

	currentAPIKey, err := o.GetCurrentAPIKey()
	if err != nil {
		return err
	}

	meta := &model.Metadata{
		Provider:  currentProvider,
		APIKey:    currentAPIKey,
		SessionID: session.ID,
	}

	return o.UpdateSessionMetadata(session, meta)
}

func (o *OpenClawAdapter) VerifyRestored(ctx context.Context, session *model.Session) (bool, error) {
	updatedMeta, err := o.GetSessionMetadata(session)
	if err != nil {
		return false, err
	}

	currentProvider, _ := o.GetCurrentProvider()
	if updatedMeta.Provider != currentProvider {
		return false, fmt.Errorf("provider mismatch")
	}

	return true, nil
}
