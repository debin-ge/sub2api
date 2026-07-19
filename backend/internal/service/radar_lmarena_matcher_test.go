package service

import (
	"math"
	"reflect"
	"testing"
)

func TestMatchLMArenaCatalogUsesControlledAliasesAndPreservesMetrics(t *testing.T) {
	vendor := "OpenAI"
	elo := 1490.5
	ciLower := 1484.25
	ciUpper := 1496.75
	votes := int64(1200)
	input := LMArenaDTO{
		Leaderboard: []LMArenaEntryDTO{
			{Rank: 1, Model: "gpt-5.5", Votes: radarMatcherInt64(1)},
			{Rank: 2, Model: "gpt-5.5-high", Votes: radarMatcherInt64(2)},
			{Rank: 3, Model: " claude-opus-4-6 ", Votes: radarMatcherInt64(3)},
			{Rank: 4, Model: "deepseek-r1", Votes: radarMatcherInt64(4)},
			{Rank: 5, Model: "o3-2025-04-16", Vendor: &vendor, Elo: &elo, CILower: &ciLower, CIUpper: &ciUpper, Votes: &votes},
			{Rank: 6, Model: "GPT-5", Votes: radarMatcherInt64(6)},
		},
		TotalVotes: radarMatcherInt64(999999),
		Stale:      true,
	}

	got := MatchLMArenaCatalog(input, []string{
		"gpt-5",
		"claude-opus-4.6",
		"astraflow/deepseek-r1",
		"o3",
	})

	if got.Stale != input.Stale {
		t.Fatalf("stale metadata changed: got %v want %v", got.Stale, input.Stale)
	}
	wantModels := []string{"claude-opus-4.6", "astraflow/deepseek-r1", "o3", "gpt-5"}
	if gotModels := radarMatcherModels(got.Leaderboard); !reflect.DeepEqual(gotModels, wantModels) {
		t.Fatalf("models = %#v, want %#v", gotModels, wantModels)
	}
	if got.Leaderboard[0].Rank != 3 || got.Leaderboard[1].Rank != 4 || got.Leaderboard[2].Rank != 5 || got.Leaderboard[3].Rank != 6 {
		t.Fatalf("absolute Arena ranks were not preserved: %#v", got.Leaderboard)
	}
	metric := got.Leaderboard[2]
	if metric.Vendor == nil || *metric.Vendor != vendor || metric.Elo == nil || *metric.Elo != elo ||
		metric.CILower == nil || *metric.CILower != ciLower || metric.CIUpper == nil || *metric.CIUpper != ciUpper ||
		metric.Votes == nil || *metric.Votes != votes {
		t.Fatalf("Arena metrics were not preserved: %#v", metric)
	}
	if got.TotalVotes == nil || *got.TotalVotes != 1213 {
		t.Fatalf("total_votes = %v, want 1213", got.TotalVotes)
	}

	// Prefixes and semantic suffixes are deliberately not fuzzy matches.
	if models := radarMatcherModels(got.Leaderboard); containsRadarMatcherModel(models, "gpt-5.5") || containsRadarMatcherModel(models, "gpt-5.5-high") {
		t.Fatalf("gpt-5 matched a different semantic model: %#v", models)
	}
	if input.Leaderboard[2].Model != " claude-opus-4-6 " || input.TotalVotes == nil || *input.TotalVotes != 999999 {
		t.Fatalf("input was mutated: %#v", input)
	}
	*metric.Votes = 0
	if *input.Leaderboard[4].Votes != votes {
		t.Fatal("result pointer aliases input metrics")
	}
}

func TestMatchLMArenaCatalogRejectsAmbiguousBestMatch(t *testing.T) {
	input := LMArenaDTO{Leaderboard: []LMArenaEntryDTO{{Rank: 1, Model: "deepseek-r1", Votes: radarMatcherInt64(10)}}}

	ambiguous := MatchLMArenaCatalog(input, []string{"vendor-a/deepseek-r1", "vendor-b/deepseek-r1"})
	if len(ambiguous.Leaderboard) != 0 {
		t.Fatalf("ambiguous namespace aliases must fail closed: %#v", ambiguous.Leaderboard)
	}

	exactWins := MatchLMArenaCatalog(input, []string{"vendor-a/deepseek-r1", "deepseek-r1", "vendor-b/deepseek-r1"})
	if models := radarMatcherModels(exactWins.Leaderboard); !reflect.DeepEqual(models, []string{"deepseek-r1"}) {
		t.Fatalf("strong exact match did not win: %#v", models)
	}
}

func TestMatchLMArenaCatalogReleaseAliasRequiresACompleteDelimitedDate(t *testing.T) {
	got := MatchLMArenaCatalog(LMArenaDTO{Leaderboard: []LMArenaEntryDTO{
		{Rank: 1, Model: "o32025-04-16", Votes: radarMatcherInt64(1)},
		{Rank: 2, Model: "o3-2025-02-30", Votes: radarMatcherInt64(2)},
		{Rank: 3, Model: "o3-20250416-preview", Votes: radarMatcherInt64(3)},
		{Rank: 4, Model: "o3-20250416", Votes: radarMatcherInt64(4)},
	}}, []string{"o3"})

	if len(got.Leaderboard) != 1 || got.Leaderboard[0].Rank != 4 || got.Leaderboard[0].Model != "o3" {
		t.Fatalf("invalid or partial date aliases were accepted: %#v", got.Leaderboard)
	}
}

func TestMatchLMArenaCatalogIsDeterministicAndDeduplicatesCatalogModels(t *testing.T) {
	firstVotes := int64(20)
	secondVotes := int64(10)
	entries := []LMArenaEntryDTO{
		{Rank: 10, Model: "gpt-4o-2024-08-06", Votes: &firstVotes},
		{Rank: 5, Model: "gpt-4o-2024-05-13", Votes: &secondVotes},
		{Rank: 3, Model: "claude-opus-4-6", Votes: radarMatcherInt64(3)},
	}
	catalog := []string{"gpt-4o", "claude-opus-4.6"}

	forward := MatchLMArenaCatalog(LMArenaDTO{Leaderboard: entries}, catalog)
	reverse := MatchLMArenaCatalog(LMArenaDTO{Leaderboard: []LMArenaEntryDTO{entries[2], entries[0], entries[1]}}, []string{catalog[1], catalog[0]})
	if !reflect.DeepEqual(forward, reverse) {
		t.Fatalf("matching depends on input order:\nforward=%#v\nreverse=%#v", forward, reverse)
	}
	if models := radarMatcherModels(forward.Leaderboard); !reflect.DeepEqual(models, []string{"claude-opus-4.6", "gpt-4o"}) {
		t.Fatalf("unexpected models: %#v", models)
	}
	if forward.Leaderboard[1].Rank != 5 || forward.Leaderboard[1].Votes == nil || *forward.Leaderboard[1].Votes != secondVotes {
		t.Fatalf("best-ranked dated Arena row was not retained: %#v", forward.Leaderboard[1])
	}
}

func TestMatchLMArenaCatalogIntersectsBeforeTakingTopTen(t *testing.T) {
	entries := make([]LMArenaEntryDTO, 0, 27)
	catalog := make([]string, 0, 12)
	for rank := 1; rank <= 15; rank++ {
		entries = append(entries, LMArenaEntryDTO{Rank: rank, Model: "unmatched-" + radarMatcherDecimal(rank), Votes: radarMatcherInt64(1)})
	}
	for rank := 16; rank <= 27; rank++ {
		model := "matched-" + radarMatcherDecimal(rank)
		entries = append(entries, LMArenaEntryDTO{Rank: rank, Model: model, Votes: radarMatcherInt64(int64(rank))})
		catalog = append(catalog, model)
	}

	got := MatchLMArenaCatalog(LMArenaDTO{Leaderboard: entries}, catalog)
	if len(got.Leaderboard) != 10 {
		t.Fatalf("leaderboard length = %d, want 10", len(got.Leaderboard))
	}
	for index, entry := range got.Leaderboard {
		wantRank := 16 + index
		if entry.Rank != wantRank {
			t.Fatalf("entry %d rank = %d, want %d", index, entry.Rank, wantRank)
		}
	}
}

func TestMatchLMArenaCatalogVoteTotalSemantics(t *testing.T) {
	tests := []struct {
		name      string
		votes     []*int64
		wantTotal *int64
	}{
		{name: "sum", votes: []*int64{radarMatcherInt64(4), radarMatcherInt64(5)}, wantTotal: radarMatcherInt64(9)},
		{name: "missing", votes: []*int64{radarMatcherInt64(4), nil}, wantTotal: nil},
		{name: "negative", votes: []*int64{radarMatcherInt64(4), radarMatcherInt64(-1)}, wantTotal: nil},
		{name: "overflow", votes: []*int64{radarMatcherInt64(math.MaxInt64), radarMatcherInt64(1)}, wantTotal: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MatchLMArenaCatalog(LMArenaDTO{Leaderboard: []LMArenaEntryDTO{
				{Rank: 1, Model: "one", Votes: tt.votes[0]},
				{Rank: 2, Model: "two", Votes: tt.votes[1]},
			}}, []string{"one", "two"})
			if !radarMatcherEqualInt64(got.TotalVotes, tt.wantTotal) {
				t.Fatalf("total_votes = %v, want %v", got.TotalVotes, tt.wantTotal)
			}
		})
	}
}

func radarMatcherInt64(value int64) *int64 { return &value }

func radarMatcherModels(entries []LMArenaEntryDTO) []string {
	models := make([]string, len(entries))
	for index := range entries {
		models[index] = entries[index].Model
	}
	return models
}

func containsRadarMatcherModel(models []string, target string) bool {
	for _, model := range models {
		if model == target {
			return true
		}
	}
	return false
}

func radarMatcherEqualInt64(left, right *int64) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func radarMatcherDecimal(value int) string {
	if value == 0 {
		return "0"
	}
	digits := make([]byte, 0, 3)
	for value > 0 {
		digits = append(digits, byte('0'+value%10))
		value /= 10
	}
	for left, right := 0, len(digits)-1; left < right; left, right = left+1, right-1 {
		digits[left], digits[right] = digits[right], digits[left]
	}
	return string(digits)
}
