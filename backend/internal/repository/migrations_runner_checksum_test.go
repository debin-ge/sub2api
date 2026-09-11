package repository

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestIsMigrationChecksumCompatible(t *testing.T) {
	t.Run("269_notx首版checksum可兼容", func(t *testing.T) {
		ok := isMigrationChecksumCompatible(
			"269_drop_video_manual_review_notx.sql",
			"ed0e2cfd37fb2b47b29d4e561d37237c0677f25732a9e154c62fcfbe0e296551",
			"27cb3b2b139b12514890c28f9130e3fb65287d1e92e08d7d813cbd3a812d2398",
		)
		require.True(t, ok)
	})

	t.Run("269_notx在未知库checksum下不兼容", func(t *testing.T) {
		ok := isMigrationChecksumCompatible(
			"269_drop_video_manual_review_notx.sql",
			"0000000000000000000000000000000000000000000000000000000000000000",
			"27cb3b2b139b12514890c28f9130e3fb65287d1e92e08d7d813cbd3a812d2398",
		)
		require.False(t, ok)
	})

	t.Run("054历史checksum可兼容", func(t *testing.T) {
		ok := isMigrationChecksumCompatible(
			"054_drop_legacy_cache_columns.sql",
			"182c193f3359946cf094090cd9e57d5c3fd9abaffbc1e8fc378646b8a6fa12b4",
			"82de761156e03876653e7a6a4eee883cd927847036f779b0b9f34c42a8af7a7d",
		)
		require.True(t, ok)
	})

	t.Run("054在未知文件checksum下不兼容", func(t *testing.T) {
		ok := isMigrationChecksumCompatible(
			"054_drop_legacy_cache_columns.sql",
			"182c193f3359946cf094090cd9e57d5c3fd9abaffbc1e8fc378646b8a6fa12b4",
			"0000000000000000000000000000000000000000000000000000000000000000",
		)
		require.False(t, ok)
	})

	t.Run("061历史checksum可兼容", func(t *testing.T) {
		ok := isMigrationChecksumCompatible(
			"061_add_usage_log_request_type.sql",
			"08a248652cbab7cfde147fc6ef8cda464f2477674e20b718312faa252e0481c0",
			"66207e7aa5dd0429c2e2c0fabdaf79783ff157fa0af2e81adff2ee03790ec65c",
		)
		require.True(t, ok)
	})

	t.Run("061第二个历史checksum可兼容", func(t *testing.T) {
		ok := isMigrationChecksumCompatible(
			"061_add_usage_log_request_type.sql",
			"222b4a09c797c22e5922b6b172327c824f5463aaa8760e4f621bc5c22e2be0f3",
			"66207e7aa5dd0429c2e2c0fabdaf79783ff157fa0af2e81adff2ee03790ec65c",
		)
		require.True(t, ok)
	})

	t.Run("非白名单迁移不兼容", func(t *testing.T) {
		ok := isMigrationChecksumCompatible(
			"001_init.sql",
			"182c193f3359946cf094090cd9e57d5c3fd9abaffbc1e8fc378646b8a6fa12b4",
			"82de761156e03876653e7a6a4eee883cd927847036f779b0b9f34c42a8af7a7d",
		)
		require.False(t, ok)
	})

	t.Run("109历史checksum可兼容", func(t *testing.T) {
		ok := isMigrationChecksumCompatible(
			"109_auth_identity_compat_backfill.sql",
			"551e498aa5616d2d91096e9d72cf9fb36e418ee22eacc557f8811cadbc9e20ee",
			"0580b4602d85435edf9aca1633db580bb3932f26517f75134106f80275ec2ace",
		)
		require.True(t, ok)
	})

	t.Run("109当前checksum可兼容历史checksum", func(t *testing.T) {
		ok := isMigrationChecksumCompatible(
			"109_auth_identity_compat_backfill.sql",
			"551e498aa5616d2d91096e9d72cf9fb36e418ee22eacc557f8811cadbc9e20ee",
			"0580b4602d85435edf9aca1633db580bb3932f26517f75134106f80275ec2ace",
		)
		require.True(t, ok)
	})

	t.Run("109回滚到历史文件后仍兼容已应用的新checksum", func(t *testing.T) {
		ok := isMigrationChecksumCompatible(
			"109_auth_identity_compat_backfill.sql",
			"0580b4602d85435edf9aca1633db580bb3932f26517f75134106f80275ec2ace",
			"551e498aa5616d2d91096e9d72cf9fb36e418ee22eacc557f8811cadbc9e20ee",
		)
		require.True(t, ok)
	})

	t.Run("110历史checksum可兼容", func(t *testing.T) {
		ok := isMigrationChecksumCompatible(
			"110_pending_auth_and_provider_default_grants.sql",
			"e3d1f433be2b564cfbdc549adf98fce13c5c7b363ebc20fd05b765d0563b0925",
			"32cf87ee787b1bb36b5c691367c96eee37518fa3eed6f3322cf68795e3745279",
		)
		require.True(t, ok)
	})

	t.Run("112历史checksum可兼容", func(t *testing.T) {
		ok := isMigrationChecksumCompatible(
			"112_add_payment_order_provider_key_snapshot.sql",
			"ffd3e8a2c9295fa9cbefefd629a78268877e5b51bc970a82d9b3f46ec4ebd15e",
			"b75f8f56d39455682787696a3d92ad25b055444ca328fb7fca9a460a15d68d99",
		)
		require.True(t, ok)
	})

	t.Run("115历史checksum可兼容修复后的legacy external backfill", func(t *testing.T) {
		ok := isMigrationChecksumCompatible(
			"115_auth_identity_legacy_external_backfill.sql",
			"4cf39e508be9fd1a5aa41610cbbebeb80385c9adda45bf78a706de9db4f1385f",
			"022aadd97bb53e755f0cf7a3a957e0cb1a1353b0c39ec4de3234acd2871fd04f",
		)
		require.True(t, ok)
	})

	t.Run("116历史checksum可兼容修复后的legacy external safety reports", func(t *testing.T) {
		ok := isMigrationChecksumCompatible(
			"116_auth_identity_legacy_external_safety_reports.sql",
			"f7757bd929ac67ffb08ce69fa4cf20fad39dbff9d5a5085fb2adabb7607e5877",
			"07edb09fa8d04ffb172b0621e3c22f4d1757d20a24ae267b3b36b087ab72d488",
		)
		require.True(t, ok)
	})

	t.Run("119历史checksum可兼容占位文件", func(t *testing.T) {
		ok := isMigrationChecksumCompatible(
			"119_enforce_payment_orders_out_trade_no_unique.sql",
			"ebd2c67cce0116393fb4f1b5d5116a67c6aceb73820dfb5133d1ff6f36d72d34",
			"0bbe809ae48a9d811dabda1ba1c74955bd71c4a9cc610f9128816818dfa6c11e",
		)
		require.True(t, ok)
	})

	t.Run("118多个历史checksum都可兼容当前版本", func(t *testing.T) {
		for _, dbChecksum := range []string{
			"a38243ca0a72c3a01c0a92b7986423054d6133c0399441f853b99802852720fb",
			"e0cdf835d6c688d64100f483d31bc02ac9ebad414bf1837af239a84bf75b8227",
		} {
			ok := isMigrationChecksumCompatible(
				"118_wechat_dual_mode_and_auth_source_defaults.sql",
				dbChecksum,
				"b54194d7a3e4fbf710e0a3590d22a2fe7966804c487052a356e0b55f53ef96b0",
			)
			require.True(t, ok)
		}
	})

	t.Run("120多个历史checksum都可兼容新的notx修复版本", func(t *testing.T) {
		for _, dbChecksum := range []string{
			"e77921f79d539bc24575cb9c16cbe566d2b23ce816190343d0a7568f6a3fcf61",
			"707431450603e70a43ce9fbd61e0c12fa67da4875158ccefabacea069587ab22",
			"04b082b5a239c525154fe9185d324ee2b05ff90da9297e10dba19f9be79aa59a",
		} {
			ok := isMigrationChecksumCompatible(
				"120_enforce_payment_orders_out_trade_no_unique_notx.sql",
				dbChecksum,
				"34aadc0db59a4e390f92a12b73bd74642d9724f33124f73638ae00089ea5e074",
			)
			require.True(t, ok)
		}
	})

	t.Run("119未知checksum不兼容", func(t *testing.T) {
		ok := isMigrationChecksumCompatible(
			"119_enforce_payment_orders_out_trade_no_unique.sql",
			"ebd2c67cce0116393fb4f1b5d5116a67c6aceb73820dfb5133d1ff6f36d72d34",
			"ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
		)
		require.False(t, ok)
	})

	t.Run("186误加入告警后的checksum兼容已发布原始迁移", func(t *testing.T) {
		ok := isMigrationChecksumCompatible(
			"186_registration_email_suffix_blacklist.sql",
			"13371a7c85985afa557c8e9679c596c1af119558274145831dc8b484865f6f29",
			"4aa7cd53e2d7d6e9a4895f232a9f92b0ffc82662de03ae00ecee1e549f7f063d",
		)
		require.True(t, ok)
	})

	t.Run("269首版checksum兼容修订版", func(t *testing.T) {
		ok := isMigrationChecksumCompatible(
			"269_drop_video_manual_review.sql",
			"92e6fb7a1926ad560e03572f0583c0827df7c7c1423641f83eada7cd41c76c1b",
			"00e601e6ff1994f3e142b216e72abd3c426574db718d4fc3037605e422f602f9",
		)
		require.True(t, ok)
	})
}

// migrationChecksumRulesWithKnownDrift 记录 fileChecksum 已经和文件内容对不上的
// 历史规则。它们随上游发布合并进来，规则常量没有跟着更新，后果是：持有该规则所列
// 历史 checksum 的库启动会直接失败（isMigrationChecksumCompatible 要求当前文件
// checksum 也落在集合内）。逐条修复需要知道各环境在野的真实 checksum，不在本次
// 变更范围内，因此显式登记而不是让不变量失效。
var migrationChecksumRulesWithKnownDrift = map[string]struct{}{
	"109_auth_identity_compat_backfill.sql":                   {},
	"110_pending_auth_and_provider_default_grants.sql":        {},
	"112_add_payment_order_provider_key_snapshot.sql":         {},
	"118_wechat_dual_mode_and_auth_source_defaults.sql":       {},
	"123_fix_legacy_auth_source_grant_on_signup_defaults.sql": {},
	"195_channel_monitor_mode.sql":                            {},
	"218_group_audio_voice_pricing.sql":                       {},
	"219_group_search_price_per_1k.sql":                       {},
	"220_clear_non_grok_video_generation_config.sql":          {},
}

// 兼容规则里的 fileChecksum 是手抄进来的常量。一旦有人再改一次迁移而忘记同步，
// 症状是应用在生产启动时 checksum mismatch 崩溃循环——这正是 269 的事故形态。
// 把常量与嵌入文件的真实内容绑死，让它退化成一条本地就能发现的失败。
func TestMigrationChecksumCompatibilityRulesMatchEmbeddedFiles(t *testing.T) {
	for name, rule := range migrationChecksumCompatibilityRules {
		content, err := migrations.FS.ReadFile(name)
		if err != nil {
			// 历史迁移可能已随功能下线一并删除，规则留着只为老库能继续启动。
			continue
		}
		sum := sha256.Sum256([]byte(strings.TrimSpace(string(content))))
		actual := hex.EncodeToString(sum[:])
		if _, drifted := migrationChecksumRulesWithKnownDrift[name]; drifted {
			// 反向断言：修好某条后测试会提示把它从登记表里删掉，避免清单越留越久。
			require.NotEqualf(t, rule.fileChecksum, actual,
				"%s 的 fileChecksum 已经对齐，请从 migrationChecksumRulesWithKnownDrift 中移除", name)
			continue
		}
		require.Equalf(t, rule.fileChecksum, actual,
			"%s 的内容与兼容规则里的 fileChecksum 不一致：改动已应用的迁移后必须同步该常量，否则已应用旧版本的库会拒绝启动", name)
	}
}
