package service

import "strings"

// token 路由准入守卫的"严格模型匹配"口径。
//
// 守卫此前问 BillingService.GetModelPricing 的是"我能不能给这个模型算出一个数"，
// 而它想问的是"有没有人给这个模型配过价"。两者的差距在查价链的最后两级：
//
//	PricingService.matchByModelFamily —— 任何含 opus/sonnet/haiku 的未知型号按关键字
//	  粗分到某个系列；
//	PricingService.matchOpenAIModel —— 剥掉 -codex/-mini/-max 后缀、试过若干静态兜底
//	  之后，把**任何** gpt- 开头的模型兜到 DefaultTestModel 上；
//	BillingService.getFallbackPricing —— 剩下任何含 claude 的一律套 claude-sonnet-4。
//
// 于是"gpt-<明年发布的新模型>"在守卫看来是"有价的"，实际按 DefaultTestModel 收费。
// 这和媒体路由的 $0.134 占位价是同一种形态：不是免费，是一个和真实上游成本无关的
// 猜测值，账面上还"正常收费"了，比记成 0 更难发现。
//
// 严格口径只认模型自己的价目条目（含大小写/前缀别名、dash↔dot 拼写归一化，以及同一
// 模型不同日期快照之间的互认），跨模型推断一律不算。准入与结算都不把推断价
// 当成真实价格证据：准入默认拒绝；若价格在上游成功后消失，结算会 fail-loud，
// 持久化为待结算，而不是借另一个 SKU 的价格凑数。
//
// 在线准入没有灰度回滚档：未知价格一旦被 shadow/off 放行，上游成本就已经真实发生，
// 无法靠事后告警恢复。pricing.strict_model_match_mode 字段只为兼容旧配置文件保留，
// 启动校验只接受 enforce，运行时守卫也不读取它。

// strictGlobalPricingGate 表示"当前账号平台的全局价目录里有没有这个模型自己的价格"。
// 管理端模型价格支持按平台覆盖，因此准入必须携带账号平台；否则平台专属覆盖只会
// 出现在管理页，在线守卫仍会把同一模型判成未定价。
type strictGlobalPricingGate struct {
	strict bool
}

func newStrictGlobalPricingGate(billingService *BillingService, platform, model string) strictGlobalPricingGate {
	return newStrictGlobalPricingGateForPlatforms(billingService, []string{platform}, model)
}

func newStrictGlobalPricingGateForPlatforms(billingService *BillingService, platforms []string, model string) strictGlobalPricingGate {
	gate := strictGlobalPricingGate{}
	model = strings.TrimSpace(model)
	if model == "" || billingService == nil {
		return gate
	}
	_, err := billingService.GetModelPricingStrictForPlatforms(platforms, model)
	gate.strict = err == nil
	return gate
}

// effective 返回在线准入唯一允许采用的严格口径。
func (g strictGlobalPricingGate) effective() bool {
	return g.strict
}
