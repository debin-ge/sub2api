//go:build unit

// pricing_diagnostic_test.go — 一次性诊断工具，用来定位公开广场
// 显示不出价格的模型：
//   1. 加载真实的 model_pricing.json（部署时用的 catalog）
//   2. 遍历所有平台 DefaultModelIDs
//   3. 分别调 PricingService.GetModelPricing 和 BillingService.getFallbackPricing
//   4. 报告命中情况和缺失字段
//
// 运行方式：
//   go test -tags=unit -v ./internal/service/ -run TestDiagnose_PricingCoverage

package service

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/Wei-Shaw/sub2api/internal/pkg/geminicli"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

// resolvePricingCatalogPath 尝试从 CWD 往上找 deploy/data/model_pricing.json；
// 否则回退到 backend/resources/model-pricing/model_prices_and_context_window.json。
func resolvePricingCatalogPath(t *testing.T) string {
	t.Helper()
	candidates := []string{
		"../../../deploy/data/model_pricing.json",
		"../../deploy/data/model_pricing.json",
		"../../../backend/resources/model-pricing/model_prices_and_context_window.json",
		"../../resources/model-pricing/model_prices_and_context_window.json",
	}
	for _, c := range candidates {
		abs, err := filepath.Abs(c)
		if err != nil {
			continue
		}
		if _, err := os.Stat(abs); err == nil {
			return abs
		}
	}
	t.Fatalf("no pricing catalog file found under known paths: %v", candidates)
	return ""
}

// enumeratePlatformModels 返回诊断范围内所有 (platform, modelID) 组合。
func enumeratePlatformModels() []struct {
	Platform string
	ModelID  string
} {
	var out []struct {
		Platform string
		ModelID  string
	}
	push := func(platform string, ids []string) {
		for _, id := range ids {
			out = append(out, struct {
				Platform string
				ModelID  string
			}{platform, id})
		}
	}
	push(PlatformAnthropic, claude.DefaultModelIDs())
	push(PlatformOpenAI, openai.DefaultModelIDs())
	geminiIDs := make([]string, 0, len(geminicli.DefaultModels))
	for _, m := range geminicli.DefaultModels {
		geminiIDs = append(geminiIDs, m.ID)
	}
	push(PlatformGemini, geminiIDs)
	antigrIDs := make([]string, 0, len(antigravity.DefaultModels()))
	for _, m := range antigravity.DefaultModels() {
		antigrIDs = append(antigrIDs, m.ID)
	}
	push(PlatformAntigravity, antigrIDs)
	for _, platform := range []string{
		PlatformMiniMax, PlatformGLM, PlatformKimi, PlatformDeepSeek,
		PlatformWindsurf, PlatformOpenCode,
	} {
		if caps, ok := GetProviderGatewayCapabilities(platform); ok {
			push(platform, caps.DefaultModelIDs)
		}
	}
	return out
}

// diagnosisRow 单条模型的诊断结果。
type diagnosisRow struct {
	Platform    string
	ModelID     string
	CatalogHit  bool
	FallbackHit bool
	MissingCard []string // 广场卡片必显的字段中缺失的（Input/Output/CacheWrite/CacheRead）
}

func TestDiagnose_PricingCoverage(t *testing.T) {
	catalogPath := resolvePricingCatalogPath(t)
	t.Logf("Using catalog: %s", catalogPath)

	data, err := os.ReadFile(catalogPath)
	if err != nil {
		t.Fatalf("read catalog: %v", err)
	}

	ps := &PricingService{
		cfg:         &config.Config{},
		pricingData: make(map[string]*ModelPriceEntry),
	}
	parsed, err := ps.parsePricingData(data)
	if err != nil {
		t.Fatalf("parse catalog: %v", err)
	}
	ps.pricingData = parsed
	t.Logf("Catalog loaded: %d entries", len(parsed))

	// 用 nil PricingService 构造 BillingService 以隔离 catalog 影响，只观察 fallback
	bs := NewBillingService(&config.Config{}, nil)

	rows := make([]diagnosisRow, 0, 64)
	for _, item := range enumeratePlatformModels() {
		row := diagnosisRow{Platform: item.Platform, ModelID: item.ModelID}

		if entry := ps.GetModelPricing(item.ModelID); entry != nil {
			row.CatalogHit = true
			if entry.InputCostPerToken == 0 {
				row.MissingCard = append(row.MissingCard, "input")
			}
			if entry.OutputCostPerToken == 0 {
				row.MissingCard = append(row.MissingCard, "output")
			}
			if entry.CacheCreationInputTokenCost == 0 {
				row.MissingCard = append(row.MissingCard, "cache_write")
			}
			if entry.CacheReadInputTokenCost == 0 {
				row.MissingCard = append(row.MissingCard, "cache_read")
			}
		}

		if fb := bs.getFallbackPricing(item.ModelID); fb != nil {
			row.FallbackHit = true
			if !row.CatalogHit {
				// 只在 catalog miss 时看 fallback 覆盖了什么字段
				if fb.InputPricePerToken == 0 {
					row.MissingCard = append(row.MissingCard, "input")
				}
				if fb.OutputPricePerToken == 0 {
					row.MissingCard = append(row.MissingCard, "output")
				}
				if fb.CacheCreationPricePerToken == 0 {
					row.MissingCard = append(row.MissingCard, "cache_write")
				}
				if fb.CacheReadPricePerToken == 0 {
					row.MissingCard = append(row.MissingCard, "cache_read")
				}
			}
		}

		rows = append(rows, row)
	}

	// 汇总输出
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Platform != rows[j].Platform {
			return rows[i].Platform < rows[j].Platform
		}
		return rows[i].ModelID < rows[j].ModelID
	})

	var (
		total, catalogHits, fallbackHits, doubleMiss int
		catalogPartial                               int
		byPlatform                                   = make(map[string]struct{ total, catalog, fallback, miss int })
	)

	fmt.Println("\n============== PRICING DIAGNOSTIC ==============")
	fmt.Printf("%-13s | %-38s | %-8s | %-8s | %s\n", "platform", "model", "catalog", "fallback", "missing_fields")
	fmt.Println(strings.Repeat("-", 100))
	for _, r := range rows {
		total++
		stat := byPlatform[r.Platform]
		stat.total++
		catalog := "-"
		if r.CatalogHit {
			catalog = "HIT"
			catalogHits++
			stat.catalog++
			if len(r.MissingCard) > 0 {
				catalogPartial++
			}
		}
		fallback := "-"
		if r.FallbackHit {
			fallback = "HIT"
			fallbackHits++
			stat.fallback++
		}
		if !r.CatalogHit && !r.FallbackHit {
			doubleMiss++
			stat.miss++
		}
		byPlatform[r.Platform] = stat

		missing := "-"
		if len(r.MissingCard) > 0 {
			missing = strings.Join(r.MissingCard, ",")
		}
		fmt.Printf("%-13s | %-38s | %-8s | %-8s | %s\n",
			r.Platform, r.ModelID, catalog, fallback, missing)
	}

	fmt.Println(strings.Repeat("-", 100))
	fmt.Printf("TOTAL          : %d\n", total)
	fmt.Printf("CATALOG hit    : %d  (partial fields %d)\n", catalogHits, catalogPartial)
	fmt.Printf("FALLBACK hit   : %d\n", fallbackHits)
	fmt.Printf("DOUBLE MISS    : %d  ← 这些模型广场既拿不到 catalog 也拿不到 fallback\n", doubleMiss)
	fmt.Println("\n-- per platform --")
	platforms := make([]string, 0, len(byPlatform))
	for p := range byPlatform {
		platforms = append(platforms, p)
	}
	sort.Strings(platforms)
	for _, p := range platforms {
		s := byPlatform[p]
		fmt.Printf("%-13s total=%-3d catalog=%-3d fallback=%-3d miss=%d\n",
			p, s.total, s.catalog, s.fallback, s.miss)
	}
	fmt.Println("================================================")
}
