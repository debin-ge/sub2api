package adapter

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
	"github.com/sub2api/session-recovery/internal/model"
)

// OpenCodeAdapter OpenCode 平台适配器（SQLite 存储）
type OpenCodeAdapter struct {
	dbPath string
}

// NewOpenCodeAdapter 创建 OpenCode 适配器
func NewOpenCodeAdapter() *OpenCodeAdapter {
	homeDir, _ := os.UserHomeDir()
	return &OpenCodeAdapter{
		dbPath: filepath.Join(homeDir, ".local", "share", "opencode", "opencode.db"),
	}
}

func (o *OpenCodeAdapter) Name() string {
	return "opencode"
}

func (o *OpenCodeAdapter) DisplayName() string {
	return "OpenCode"
}

func (o *OpenCodeAdapter) DefaultSessionPath() string {
	return o.dbPath
}

func (o *OpenCodeAdapter) IsInstalled() bool {
	_, err := os.Stat(o.dbPath)
	return err == nil
}

func (o *OpenCodeAdapter) GetCurrentProvider() (string, error) {
	db, err := sql.Open("sqlite3", o.dbPath)
	if err != nil {
		return "", err
	}
	defer db.Close()

	var provider string
	err = db.QueryRow("SELECT value FROM config WHERE key = 'provider'").Scan(&provider)
	if err != nil {
		return "Unknown", nil
	}

	return provider, nil
}

func (o *OpenCodeAdapter) GetCurrentAPIKey() (string, error) {
	db, err := sql.Open("sqlite3", o.dbPath)
	if err != nil {
		return "", err
	}
	defer db.Close()

	var apiKey string
	err = db.QueryRow("SELECT value FROM config WHERE key = 'api_key'").Scan(&apiKey)
	if err != nil {
		return "", fmt.Errorf("api_key not found")
	}

	return apiKey, nil
}

func (o *OpenCodeAdapter) ScanAllSessions(ctx context.Context) ([]*model.Session, error) {
	db, err := sql.Open("sqlite3", o.dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(`
		SELECT
			session_id,
			project,
			provider,
			created_at,
			updated_at,
			message_count,
			first_message
		FROM sessions
		ORDER BY updated_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []*model.Session
	for rows.Next() {
		session := &model.Session{
			Platform: "opencode",
			FilePath: o.dbPath,
		}

		var createdAt, updatedAt string
		err := rows.Scan(
			&session.ID,
			&session.Project,
			&session.OriginalProvider,
			&createdAt,
			&updatedAt,
			&session.MessageCount,
			&session.FirstMessage,
		)
		if err != nil {
			continue
		}

		sessions = append(sessions, session)
	}

	return sessions, nil
}

func (o *OpenCodeAdapter) ParseSessionFile(ctx context.Context, path string) (*model.Session, error) {
	return nil, fmt.Errorf("OpenCode uses SQLite, not files")
}

func (o *OpenCodeAdapter) GetSessionMetadata(session *model.Session) (*model.Metadata, error) {
	db, err := sql.Open("sqlite3", o.dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	meta := &model.Metadata{
		SessionID: session.ID,
		Custom:    make(map[string]interface{}),
	}

	var provider, apiKey string
	err = db.QueryRow(`
		SELECT provider, api_key
		FROM sessions
		WHERE session_id = ?
	`, session.ID).Scan(&provider, &apiKey)

	if err != nil {
		return nil, err
	}

	meta.Provider = provider
	meta.APIKey = apiKey

	return meta, nil
}

func (o *OpenCodeAdapter) UpdateSessionMetadata(session *model.Session, meta *model.Metadata) error {
	db, err := sql.Open("sqlite3", o.dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	_, err = db.Exec(`
		UPDATE sessions
		SET provider = ?, api_key = ?, updated_at = CURRENT_TIMESTAMP
		WHERE session_id = ?
	`, meta.Provider, meta.APIKey, session.ID)

	return err
}

func (o *OpenCodeAdapter) RestoreSession(ctx context.Context, session *model.Session) error {
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

func (o *OpenCodeAdapter) VerifyRestored(ctx context.Context, session *model.Session) (bool, error) {
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
