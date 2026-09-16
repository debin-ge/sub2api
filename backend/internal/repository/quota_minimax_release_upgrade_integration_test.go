//go:build integration

package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"io/fs"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// 生产（release 血脉）升级到合并后的镜像时卡在
//
//	apply migration 267_user_platform_quotas_add_bytedance.sql: pq: check constraint
//	"user_platform_quotas_platform_check" of relation "user_platform_quotas" is
//	violated by some row
//
// 反复重启：237 早就在 release 血脉上放开了 minimax，库里存在 platform='minimax' 的
// 配额行，而 267 把同一个约束重建为不含 minimax 的集合；273 的并集版排在 267 之后，
// 救不了它。266a 把 267 登记为已应用来跳过，终态由 273 写定。
//
// 这个用例按生产的真实形态搭基线：迁移止于 237 之前 + 一条 minimax 配额行。
func TestReleaseUpgradeWithExistingMinimaxQuotaRow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	name := "quota_minimax_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err := integrationDB.ExecContext(ctx, `CREATE DATABASE `+pq.QuoteIdentifier(name))
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupCtx, release := context.WithTimeout(context.Background(), 10*time.Second)
		defer release()
		_, err := integrationDB.ExecContext(cleanupCtx, `DROP DATABASE `+pq.QuoteIdentifier(name)+` WITH (FORCE)`)
		require.NoError(t, err)
	})
	dsn, err := url.Parse(integrationDSN)
	require.NoError(t, err)
	dsn.Path = "/" + name
	db, err := sql.Open("postgres", dsn.String())
	require.NoError(t, err)
	defer db.Close()

	// 生产基线：release 血脉迁移止于 241，平台 CHECK 停在 237 的取值（含 minimax）。
	baseline := fstest.MapFS{}
	entries, err := fs.ReadDir(migrations.FS, ".")
	require.NoError(t, err)
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".sql") || entry.Name() >= "238_" {
			continue
		}
		body, err := fs.ReadFile(migrations.FS, entry.Name())
		require.NoError(t, err)
		baseline[entry.Name()] = &fstest.MapFile{Data: body}
	}
	require.NotEmpty(t, baseline)
	require.NoError(t, applyMigrationsFS(ctx, db, baseline))

	var userID int64
	require.NoError(t, db.QueryRowContext(ctx,
		`INSERT INTO users(email,password_hash,balance) VALUES('minimax-quota@example.invalid','synthetic',10) RETURNING id`,
	).Scan(&userID))
	var quotaID int64
	require.NoError(t, db.QueryRowContext(ctx,
		`INSERT INTO user_platform_quotas(user_id,platform,daily_limit_usd) VALUES($1,'minimax',12.5) RETURNING id`,
		userID,
	).Scan(&quotaID), "基线上必须能写入 minimax 配额 —— 这正是生产库的状态")

	// 修复前这一步会在 267 上报 check constraint ... is violated by some row。
	require.NoError(t, ApplyMigrations(ctx, db))
	require.NoError(t, ApplyMigrations(ctx, db), "a repeated migration run must be a no-op")

	// minimax 配额行必须原样保留：跳过 267 靠的是记账，不是删数据。
	var platform string
	var dailyLimit sql.NullFloat64
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT platform,daily_limit_usd FROM user_platform_quotas WHERE id=$1`, quotaID,
	).Scan(&platform, &dailyLimit))
	require.Equal(t, "minimax", platform)
	require.True(t, dailyLimit.Valid)
	require.InDelta(t, 12.5, dailyLimit.Float64, 1e-9)

	// 终态由 273 写定：minimax 与 bytedance 都必须放行。
	_, err = db.ExecContext(ctx,
		`INSERT INTO user_platform_quotas(user_id,platform) VALUES($1,'bytedance')`, userID)
	require.NoError(t, err, "273 的并集约束必须放行 bytedance")

	// 267 被登记为已应用，且 checksum 与文件一致 —— 否则下次启动会变成 checksum mismatch。
	body, err := fs.ReadFile(migrations.FS, "267_user_platform_quotas_add_bytedance.sql")
	require.NoError(t, err)
	sum := sha256.Sum256([]byte(strings.TrimSpace(string(body))))
	var recorded string
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT checksum FROM schema_migrations WHERE filename='267_user_platform_quotas_add_bytedance.sql'`,
	).Scan(&recorded))
	require.Equal(t, hex.EncodeToString(sum[:]), recorded)
}
