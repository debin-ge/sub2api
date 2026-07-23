//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUserRepository_Create_ReusesOuterTransaction(t *testing.T) {
	ctx := context.Background()
	tx, err := integrationEntClient.Tx(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	txCtx := dbent.NewTxContext(ctx, tx)

	repo := NewUserRepository(integrationEntClient, integrationDB)
	email := fmt.Sprintf("outer-tx-registration-%d@example.com", time.Now().UnixNano())
	user := &service.User{
		Email:        email,
		PasswordHash: "hash",
		Role:         service.RoleUser,
		Status:       service.StatusActive,
	}
	require.NoError(t, repo.Create(txCtx, user))
	require.Positive(t, user.ID)

	require.NoError(t, tx.Rollback())

	rows, err := integrationEntClient.QueryContext(ctx, `
SELECT COUNT(*)
FROM users
WHERE id = $1`, user.ID)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	require.True(t, rows.Next())
	var count int
	require.NoError(t, rows.Scan(&count))
	require.Zero(t, count, "user creation must roll back with the caller's transaction")
}
