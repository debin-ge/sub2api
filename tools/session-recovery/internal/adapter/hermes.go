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

// HermesAdapter Hermes 平台适配器（SQLite 存储）
type HermesAdapter struct {
	dbPath string
}

// NewHermesAdapter 创建 Hermes 适配器
func NewHermesAdapter() *HermesAdapter {
	homeDir, _ := os.UserHomeDir()
	return &HermesAdapter{
		dbPath: filepath.Join(homeDir, ".hermes", "state.db"),
	}
}

func (h *HermesAdapter) Name() string {
	return "hermes"
}

func (h *HermesAdapter) DisplayName() string {
	return "Hermes"
}

func (h *HermesAdapter) DefaultSessionPath() string {
	return h.dbPath
}

func (h *HermesAdapter) IsInstalled() bool {
	_, err := os.Stat(h.dbPath)
	return err == nil
}

func (h *HermesAdapter) GetCurrentProvider() (string, error) {
	db, err := sql.Open("sqlite3", h.dbPath)
	if err != nil {
		return "", err
	}
	defer db.Close()

	var provider string
	err = db.QueryRow("SELECT value FROM settings WHERE key = 'provider'").Scan(&provider)
	if err != nil {
		return "Unknown", nil
	}

	return provider, nil
}

func (h *HermesAdapter) GetCurrentAPIKey() (string, error) {
	db, err := sql.Open("sqlite3", h.dbPath)
	if err != nil {
		return "", err
	}
	defer db.Close()

	var apiKey string
	err = db.QueryRow("SELECT value FROM settings WHERE key = 'api_key'").Scan(&apiKey)
	if err != nil {
		return "", fmt.Errorf("api_key not found")
	}

	return apiKey, nil
}

func (h *HermesAdapter) ScanAllSessions(ctx context.Context) ([]*model.Session, error) {
	db, err := sql.Open("sqlite3", h.dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(`
		SELECT
			id,
			project_name,
			provider,
			created_at,
			updated_at,
			message_count,
			first_message
		FROM conversations
		ORDER BY updated_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []*model.Session
	for rows.Next() {
		session := &model.Session{
			Platform: "hermes",
			FilePath: h.dbPath,
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

func (h *HermesAdapter) ParseSessionFile(ctx context.Context, path string) (*model.Session, error) {
	return nil, fmt.Errorf("Hermes uses SQLite, not files")
}

func (h *HermesAdapter) GetSessionMetadata(session *model.Session) (*model.Metadata, error) {
	db, err := sql.Open("sqlite3", h.dbPath)
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
		FROM conversations
		WHERE id = ?
	`, session.ID).Scan(&provider, &apiKey)

	if err != nil {
		return nil, err
	}

	meta.Provider = provider
	meta.APIKey = apiKey

	return meta, nil
}

func (h *HermesAdapter) UpdateSessionMetadata(session *model.Session, meta *model.Metadata) error {
	db, err := sql.Open("sqlite3", h.dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	_, err = db.Exec(`
		UPDATE conversations
		SET provider = ?, api_key = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, meta.Provider, meta.APIKey, session.ID)

	return err
}

func (h *HermesAdapter) RestoreSession(ctx context.Context, session *model.Session) error {
	currentProvider, err := h.GetCurrentProvider()
	if err != nil {
		return err
	}

	currentAPIKey, err := h.GetCurrentAPIKey()
	if err != nil {
		return err
	}

	meta := &model.Metadata{
		Provider:  currentProvider,
		APIKey:    currentAPIKey,
		SessionID: session.ID,
	}

	return h.UpdateSessionMetadata(session, meta)
}

func (h *HermesAdapter) VerifyRestored(ctx context.Context, session *model.Session) (bool, error) {
	updatedMeta, err := h.GetSessionMetadata(session)
	if err != nil {
		return false, err
	}

	currentProvider, _ := h.GetCurrentProvider()
	if updatedMeta.Provider != currentProvider {
		return false, fmt.Errorf("provider mismatch")
	}

	return true, nil
}
