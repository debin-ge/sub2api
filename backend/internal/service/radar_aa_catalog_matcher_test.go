package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func aaCatalogTestModel(slug string, score float64) ArtificialAnalysisModel {
	intelligence, coding, agentic := score, score, score
	return ArtificialAnalysisModel{
		Slug: slug, Name: "AA " + slug, Creator: "AA Vendor",
		IntelligenceIndex: &intelligence,
		CodingIndex:       &coding,
		AgenticIndex:      &agentic,
	}
}

func TestMatchArtificialAnalysisCatalogPrefersFewerSemanticSuffixRemovals(t *testing.T) {
	models := []ArtificialAnalysisModel{
		aaCatalogTestModel("gpt-5", 95),
		aaCatalogTestModel("gpt-5-pro", 90),
	}

	result, err := MatchArtificialAnalysisCatalog(models, map[string][]string{
		"openai": {"openai/gpt-5-pro-high"},
	})

	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Equal(t, "gpt-5-pro", result[0].Slug)
	require.Equal(t, []DegradationCatalogMatchDTO{{
		Platform: "openai", ModelID: "openai/gpt-5-pro-high",
	}}, result[0].CatalogMatches)
}

func TestMatchArtificialAnalysisCatalogSupportsBothSidesAndTwoSuffixes(t *testing.T) {
	tests := []struct {
		name      string
		aaSlug    string
		catalogID string
	}{
		{name: "AA side suffix", aaSlug: "gpt-5-pro-high", catalogID: "gpt-5-pro"},
		{name: "catalog side two suffixes", aaSlug: "gpt-5", catalogID: "gpt-5-pro-high"},
		{name: "AA side two suffixes", aaSlug: "gpt-5-pro-high", catalogID: "gpt-5"},
		{name: "one suffix from both sides", aaSlug: "gpt-5-pro", catalogID: "gpt-5-high"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := MatchArtificialAnalysisCatalog(
				[]ArtificialAnalysisModel{aaCatalogTestModel(tt.aaSlug, 90)},
				map[string][]string{"openai": {tt.catalogID}},
			)
			require.NoError(t, err)
			require.Len(t, result, 1)
			require.Equal(t, tt.aaSlug, result[0].Slug)
		})
	}
}

func TestMatchArtificialAnalysisCatalogRejectsMoreThanTwoCombinedSuffixRemovals(t *testing.T) {
	result, err := MatchArtificialAnalysisCatalog(
		[]ArtificialAnalysisModel{aaCatalogTestModel("x-pro-high", 90)},
		map[string][]string{"openai": {"x-thinking-max"}},
	)

	require.NoError(t, err)
	require.Empty(t, result)
}

func TestMatchArtificialAnalysisCatalogExactBeatsFuzzyAndAmbiguityIsExcluded(t *testing.T) {
	t.Run("exact wins", func(t *testing.T) {
		result, err := MatchArtificialAnalysisCatalog([]ArtificialAnalysisModel{
			aaCatalogTestModel("gpt-5", 99),
			aaCatalogTestModel("gpt-5-high", 90),
		}, map[string][]string{"openai": {"gpt-5-high"}})
		require.NoError(t, err)
		require.Len(t, result, 1)
		require.Equal(t, "gpt-5-high", result[0].Slug)
	})

	t.Run("equal strength ambiguity", func(t *testing.T) {
		result, err := MatchArtificialAnalysisCatalog([]ArtificialAnalysisModel{
			aaCatalogTestModel("gpt-5-pro", 90),
			aaCatalogTestModel("gpt-5-high", 90),
		}, map[string][]string{"openai": {"gpt-5-ultra"}})
		require.NoError(t, err)
		require.Empty(t, result)
	})
}

func TestMatchArtificialAnalysisCatalogRemovesCompleteReleaseDatesOnBothSides(t *testing.T) {
	result, err := MatchArtificialAnalysisCatalog(
		[]ArtificialAnalysisModel{aaCatalogTestModel("gpt-5-2026-06-30", 90)},
		map[string][]string{"openai": {"openai/gpt-5-2026-07-15"}},
	)

	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Equal(t, "gpt-5-2026-06-30", result[0].Slug)
	require.Equal(t, "openai/gpt-5-2026-07-15", result[0].CatalogMatches[0].ModelID)
}

func TestMatchArtificialAnalysisCatalogExcludesIncompleteMetricsAndNonSemanticSuffixes(t *testing.T) {
	incomplete := aaCatalogTestModel("complete-looking", 90)
	incomplete.AgenticIndex = nil
	result, err := MatchArtificialAnalysisCatalog([]ArtificialAnalysisModel{
		incomplete,
		aaCatalogTestModel("gpt-5", 80),
	}, map[string][]string{
		"openai": {"complete-looking", "gpt-5-codex", "gpt-5-preview", "gpt-5-pro-high-ultra"},
	})

	require.NoError(t, err)
	require.Empty(t, result, "missing metrics, product suffixes, and more than two suffix removals must not match")
}

func TestMatchArtificialAnalysisCatalogGroupsVariantsAndPreservesOriginalValues(t *testing.T) {
	model := aaCatalogTestModel("GPT.5-Pro", 90)
	model.Name = "GPT 5 Pro (AA Original)"
	result, err := MatchArtificialAnalysisCatalog([]ArtificialAnalysisModel{model}, map[string][]string{
		"OpenAI": {
			"OpenAI/GPT.5-Pro-XHigh",
			"OpenAI/GPT.5-Pro-Low",
			"OpenAI/GPT.5-Pro-High",
			"OpenAI/GPT.5-Pro-High",
		},
	})

	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Equal(t, "GPT.5-Pro", result[0].Slug)
	require.Equal(t, "GPT 5 Pro (AA Original)", result[0].Name)
	require.Equal(t, []DegradationCatalogMatchDTO{
		{Platform: "OpenAI", ModelID: "OpenAI/GPT.5-Pro-High"},
		{Platform: "OpenAI", ModelID: "OpenAI/GPT.5-Pro-Low"},
		{Platform: "OpenAI", ModelID: "OpenAI/GPT.5-Pro-XHigh"},
	}, result[0].CatalogMatches)
}

func TestMatchArtificialAnalysisCatalogSortsByThreeMetricAverageThenSlug(t *testing.T) {
	alpha := aaCatalogTestModel("alpha", 80)
	alpha.IntelligenceIndex = aaCatalogTestMetric(100)
	alpha.CodingIndex = aaCatalogTestMetric(70)
	alpha.AgenticIndex = aaCatalogTestMetric(70)
	bravo := aaCatalogTestModel("bravo", 80)
	top := aaCatalogTestModel("top", 90)

	result, err := MatchArtificialAnalysisCatalog(
		[]ArtificialAnalysisModel{bravo, top, alpha},
		map[string][]string{"platform": {"bravo", "top", "alpha"}},
	)

	require.NoError(t, err)
	require.Equal(t, []string{"top", "alpha", "bravo"}, []string{
		result[0].Slug, result[1].Slug, result[2].Slug,
	})
}

func TestMatchArtificialAnalysisCatalogRejectsInvalidAAInput(t *testing.T) {
	model := aaCatalogTestModel("duplicate", 90)
	result, err := MatchArtificialAnalysisCatalog(
		[]ArtificialAnalysisModel{model, model},
		map[string][]string{"openai": {"duplicate"}},
	)
	require.Error(t, err)
	require.Nil(t, result)
}

func aaCatalogTestMetric(value float64) *float64 {
	return &value
}
