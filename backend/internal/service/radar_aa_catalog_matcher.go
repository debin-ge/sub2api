package service

import (
	"math"
	"sort"
	"strings"
)

const (
	radarAAMaxSemanticSuffixesPerSide  = 2
	radarAAMaxSemanticSuffixesCombined = 2
)

var radarAASemanticSuffixes = map[string]struct{}{
	"high": {}, "low": {}, "medium": {}, "xhigh": {},
	"thinking": {}, "reasoning": {}, "pro": {}, "mini": {},
	"max": {}, "ultra": {},
}

type artificialAnalysisCatalogCandidate struct {
	model              ArtificialAnalysisModel
	raw                string
	folded             string
	canonical          string
	releaseAlias       string
	semanticAliases    []radarAASemanticAlias
	releaseAliasSuffix []radarAASemanticAlias
}

type radarAASemanticAlias struct {
	value   string
	removed int
}

type radarAAMatchScore struct {
	class   int
	removed int
}

// MatchArtificialAnalysisCatalog keeps only complete AA benchmark records
// which have an unambiguous match in the passive public Model Plaza catalog.
// Matching aliases are temporary: AA names/slugs and catalog IDs are always
// returned exactly as supplied by their respective sources.
func MatchArtificialAnalysisCatalog(
	models []ArtificialAnalysisModel,
	byPlatform map[string][]string,
) ([]DegradationModelDTO, error) {
	if err := validateArtificialAnalysisModels(models); err != nil {
		return nil, errInvalidArtificialAnalysisModelsResponse
	}
	candidates := normalizeArtificialAnalysisCatalogCandidates(models)
	matches := make(map[string][]DegradationCatalogMatchDTO, len(candidates))
	seenMatches := make(map[string]struct{})

	platforms := make([]string, 0, len(byPlatform))
	for platform := range byPlatform {
		platforms = append(platforms, platform)
	}
	sort.Strings(platforms)
	for _, platform := range platforms {
		for _, catalogID := range normalizeCatalogModelIDs(byPlatform[platform]) {
			if IsPublicCatalogRoutingOnlyModelID(catalogID) {
				continue
			}
			aaSlug, ok := matchArtificialAnalysisModel(catalogID, candidates)
			if !ok {
				continue
			}
			dedupeKey := aaSlug + "\x00" + platform + "\x00" + strings.ToLower(catalogID)
			if _, exists := seenMatches[dedupeKey]; exists {
				continue
			}
			seenMatches[dedupeKey] = struct{}{}
			matches[aaSlug] = append(matches[aaSlug], DegradationCatalogMatchDTO{
				ModelID: catalogID, Platform: platform,
			})
		}
	}

	result := make([]DegradationModelDTO, 0, len(matches))
	for _, item := range candidates {
		catalogMatches := matches[item.model.Slug]
		if len(catalogMatches) == 0 {
			continue
		}
		sort.Slice(catalogMatches, func(i, j int) bool {
			if catalogMatches[i].Platform != catalogMatches[j].Platform {
				return catalogMatches[i].Platform < catalogMatches[j].Platform
			}
			left := strings.ToLower(catalogMatches[i].ModelID)
			right := strings.ToLower(catalogMatches[j].ModelID)
			if left != right {
				return left < right
			}
			return catalogMatches[i].ModelID < catalogMatches[j].ModelID
		})
		mapped, err := mapArtificialAnalysisModel(item.model)
		if err != nil {
			return nil, err
		}
		mapped.CatalogMatches = catalogMatches
		result = append(result, mapped)
	}
	sort.Slice(result, func(i, j int) bool {
		left := degradationModelAverage(result[i])
		right := degradationModelAverage(result[j])
		if left != right {
			return left > right
		}
		leftSlug := strings.ToLower(result[i].Slug)
		rightSlug := strings.ToLower(result[j].Slug)
		if leftSlug != rightSlug {
			return leftSlug < rightSlug
		}
		return result[i].Slug < result[j].Slug
	})
	return result, nil
}

func normalizeArtificialAnalysisCatalogCandidates(models []ArtificialAnalysisModel) []artificialAnalysisCatalogCandidate {
	result := make([]artificialAnalysisCatalogCandidate, 0, len(models))
	for _, model := range models {
		if model.IntelligenceIndex == nil || model.CodingIndex == nil || model.AgenticIndex == nil {
			continue
		}
		raw := strings.TrimSpace(model.Slug)
		canonical := canonicalRadarLMArenaModelID(raw)
		if canonical == "" {
			continue
		}
		releaseAlias := radarLMArenaReleaseAlias(canonical)
		result = append(result, artificialAnalysisCatalogCandidate{
			model:              model,
			raw:                raw,
			folded:             strings.ToLower(raw),
			canonical:          canonical,
			releaseAlias:       releaseAlias,
			semanticAliases:    radarAASemanticAliases(canonical),
			releaseAliasSuffix: radarAASemanticAliases(releaseAlias),
		})
	}
	return result
}

func matchArtificialAnalysisModel(catalogID string, candidates []artificialAnalysisCatalogCandidate) (string, bool) {
	raw := strings.TrimSpace(catalogID)
	if raw == "" {
		return "", false
	}
	folded := strings.ToLower(raw)
	canonical := canonicalRadarLMArenaModelID(raw)
	namespaceCanonical := ""
	if slash := strings.LastIndex(raw, "/"); slash >= 0 && slash+1 < len(raw) {
		namespaceCanonical = canonicalRadarLMArenaModelID(raw[slash+1:])
	}
	if canonical == "" {
		return "", false
	}
	structuralCanonical := canonical
	if namespaceCanonical != "" {
		structuralCanonical = namespaceCanonical
	}
	releaseAlias := radarLMArenaReleaseAlias(structuralCanonical)
	catalogAliases := radarAASemanticAliases(structuralCanonical)
	catalogReleaseAliases := radarAASemanticAliases(releaseAlias)

	bestScore := radarAAMatchScore{class: math.MaxInt, removed: math.MaxInt}
	best := make(map[string]struct{})
	for _, item := range candidates {
		score, ok := scoreArtificialAnalysisCatalogMatch(
			raw, folded, canonical, namespaceCanonical, releaseAlias,
			catalogAliases, catalogReleaseAliases, item,
		)
		if !ok || lessRadarAAMatchScore(bestScore, score) {
			continue
		}
		if lessRadarAAMatchScore(score, bestScore) {
			bestScore = score
			clear(best)
		}
		best[item.model.Slug] = struct{}{}
	}
	if len(best) != 1 {
		return "", false
	}
	for slug := range best {
		return slug, true
	}
	return "", false
}

func scoreArtificialAnalysisCatalogMatch(
	raw, folded, canonical, namespaceCanonical, releaseAlias string,
	catalogAliases, catalogReleaseAliases []radarAASemanticAlias,
	item artificialAnalysisCatalogCandidate,
) (radarAAMatchScore, bool) {
	switch {
	case raw == item.raw:
		return radarAAMatchScore{class: 0}, true
	case folded == item.folded:
		return radarAAMatchScore{class: 1}, true
	case canonical == item.canonical:
		return radarAAMatchScore{class: 2}, true
	case namespaceCanonical != "" && namespaceCanonical == item.canonical:
		return radarAAMatchScore{class: 3}, true
	case releaseAlias != "" && releaseAlias == item.canonical:
		return radarAAMatchScore{class: 4}, true
	case item.releaseAlias != "" && item.releaseAlias == canonical:
		return radarAAMatchScore{class: 4}, true
	case item.releaseAlias != "" && namespaceCanonical != "" && item.releaseAlias == namespaceCanonical:
		return radarAAMatchScore{class: 4}, true
	case releaseAlias != "" && item.releaseAlias != "" && releaseAlias == item.releaseAlias:
		return radarAAMatchScore{class: 4}, true
	}

	bestRemoved := math.MaxInt
	for _, left := range appendSemanticBase(catalogAliases, releaseAlias, catalogReleaseAliases) {
		for _, right := range appendSemanticBase(item.semanticAliases, item.releaseAlias, item.releaseAliasSuffix) {
			removed := left.removed + right.removed
			if removed == 0 || removed > radarAAMaxSemanticSuffixesCombined || left.value == "" || left.value != right.value {
				continue
			}
			if removed < bestRemoved {
				bestRemoved = removed
			}
		}
	}
	if bestRemoved == math.MaxInt {
		return radarAAMatchScore{}, false
	}
	return radarAAMatchScore{class: 5, removed: bestRemoved}, true
}

func appendSemanticBase(aliases []radarAASemanticAlias, releaseAlias string, releaseAliases []radarAASemanticAlias) []radarAASemanticAlias {
	result := make([]radarAASemanticAlias, 0, len(aliases)+len(releaseAliases)+1)
	result = append(result, aliases...)
	if releaseAlias != "" {
		result = append(result, radarAASemanticAlias{value: releaseAlias})
	}
	result = append(result, releaseAliases...)
	return result
}

func radarAASemanticAliases(canonical string) []radarAASemanticAlias {
	if canonical == "" {
		return nil
	}
	result := []radarAASemanticAlias{{value: canonical}}
	current := canonical
	for removed := 1; removed <= radarAAMaxSemanticSuffixesPerSide; removed++ {
		separator := strings.LastIndexByte(current, '-')
		if separator <= 0 || separator+1 >= len(current) {
			break
		}
		if _, allowed := radarAASemanticSuffixes[current[separator+1:]]; !allowed {
			break
		}
		current = current[:separator]
		result = append(result, radarAASemanticAlias{value: current, removed: removed})
	}
	return result
}

func lessRadarAAMatchScore(left, right radarAAMatchScore) bool {
	if left.class != right.class {
		return left.class < right.class
	}
	return left.removed < right.removed
}

func degradationModelAverage(model DegradationModelDTO) float64 {
	return (*model.IntelligenceIndex + *model.CodingIndex + *model.AgenticIndex) / 3
}
