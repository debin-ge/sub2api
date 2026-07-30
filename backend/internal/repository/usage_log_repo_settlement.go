package repository

import (
	"context"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// 待结算用量的读写。两个方法都只碰 billing_state = 1 的行——也就是部分索引
// idx_usage_logs_billing_state_pending 覆盖的那一小撮，与在线写入路径几乎不重叠。

// ListPendingSettlement 按 id 升序取出待补结算的用量记录。
//
// 不复用 ListWithFilters 是因为那条路会额外做 COUNT(*) 和关联对象 hydrate，而补偿
// 任务两样都不需要：它只要行本身，一轮扫不完的下一轮接着扫。
//
// 升序 + afterID 游标而不是 OFFSET 分页：补偿会把处理过的行改成 billing_state=2，
// 从结果集里消失，OFFSET 会让后面的行整体前移而被跳过。游标不受集合收缩影响。
func (r *usageLogRepository) ListPendingSettlement(ctx context.Context, afterID int64, limit int) ([]service.UsageLog, error) {
	if limit <= 0 {
		return nil, nil
	}
	query := fmt.Sprintf(
		"SELECT %s FROM usage_logs WHERE billing_state = %d AND id > $1 ORDER BY id ASC LIMIT $2",
		usageLogSelectColumns,
		service.BillingStatePricingUnavailable,
	)
	return r.queryUsageLogs(ctx, query, afterID, limit)
}

// MarkSettlementRecovered 把一条待结算记录改写为“价格已恢复”。
//
// WHERE 里带 billing_state = 1 是幂等闸门：两个补偿轮次（或两个实例）同时扫到同一行时，
// 第二次的 UPDATE 影响 0 行而不是把金额再加一遍。返回的 bool 就是"这次真的改到了"。
//
// 只写标准费用字段与 billing_state，不动 tokens/模型/时间戳：那些是请求当时的事实，
// 补的是价格，不是用量。actual_cost 明确保持 0，因为该任务没有执行余额、订阅或配额
// 扣款；理论应收绝不能伪装成已经发生的实际扣除。
func (r *usageLogRepository) MarkSettlementRecovered(ctx context.Context, id int64, cost service.SettlementCost) (bool, error) {
	const query = `
		UPDATE usage_logs SET
			input_cost = $2,
			image_input_cost = $3,
			output_cost = $4,
			cache_creation_cost = $5,
			cache_read_cost = $6,
			image_output_cost = $7,
			total_cost = $8,
			actual_cost = 0,
			account_stats_cost = $9,
			billing_mode = $10,
			billing_state = $11
		WHERE id = $1 AND billing_state = $12`
	res, err := r.sql.ExecContext(ctx, query,
		id,
		cost.InputCost,
		cost.ImageInputCost,
		cost.OutputCost,
		cost.CacheCreationCost,
		cost.CacheReadCost,
		cost.ImageOutputCost,
		cost.TotalCost,
		cost.AccountStatsCost,
		cost.BillingMode,
		int16(service.BillingStatePricingRecovered),
		int16(service.BillingStatePricingUnavailable),
	)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}
