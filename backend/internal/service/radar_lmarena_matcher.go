package service

import (
	"math"
	"sort"
	"strings"
	"time"
	"unicode"
)

const radarLMArenaCatalogLimit = 10

type radarLMArenaCatalogModel struct {
	display            string
	raw                string
	canonical          string
	namespaceCanonical string
}

type radarLMArenaCatalogMatch struct {
	entry      LMArenaEntryDTO
	arenaModel string
	catalogID  string
}

// MatchLMArenaCatalog intersects a complete Arena leaderboard with model IDs
// exposed by the public model catalog. Matching is deliberately narrower than
// general fuzzy search: it permits case, separator, catalog namespace, and a
// complete trailing release-date alias, but never strips semantic model
// suffixes such as high, thinking, mini, or pro.
//
// The result is a deep copy, retains absolute Arena ranks and metrics, is
// deduplicated by the selected catalog ID, and is limited only after the full
// intersection has been ranked.
func MatchLMArenaCatalog(input LMArenaDTO, catalogModelIDs []string) LMArenaDTO {
	catalog := normalizeRadarLMArenaCatalog(catalogModelIDs)
	matches := make([]radarLMArenaCatalogMatch, 0, len(input.Leaderboard))
	for _, entry := range input.Leaderboard {
		catalogID, ok := matchRadarLMArenaCatalogModel(entry.Model, catalog)
		if !ok {
			continue
		}
		matches = append(matches, radarLMArenaCatalogMatch{
			entry:      cloneRadarLMArenaCatalogEntry(entry),
			arenaModel: entry.Model,
			catalogID:  catalogID,
		})
	}

	sort.Slice(matches, func(i, j int) bool {
		return lessRadarLMArenaCatalogMatch(matches[i], matches[j])
	})

	leaderboard := make([]LMArenaEntryDTO, 0, min(len(matches), radarLMArenaCatalogLimit))
	seenCatalogIDs := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		catalogKey := strings.ToLower(strings.TrimSpace(match.catalogID))
		if _, exists := seenCatalogIDs[catalogKey]; exists {
			continue
		}
		seenCatalogIDs[catalogKey] = struct{}{}
		entry := match.entry
		entry.Model = match.catalogID
		leaderboard = append(leaderboard, entry)
		if len(leaderboard) == radarLMArenaCatalogLimit {
			break
		}
	}

	result := LMArenaDTO{
		Leaderboard:   leaderboard,
		TotalVotes:    radarLMArenaCatalogVoteTotal(leaderboard),
		LastUpdatedAt: cloneRadarLMArenaCatalogTime(input.LastUpdatedAt),
		FetchedAt:     cloneRadarLMArenaCatalogTime(input.FetchedAt),
		Stale:         input.Stale,
	}
	return result
}

func normalizeRadarLMArenaCatalog(modelIDs []string) []radarLMArenaCatalogModel {
	models := make([]radarLMArenaCatalogModel, 0, len(modelIDs))
	seen := make(map[string]struct{}, len(modelIDs))
	for _, modelID := range modelIDs {
		display := strings.TrimSpace(modelID)
		if display == "" {
			continue
		}
		raw := strings.ToLower(display)
		if _, exists := seen[raw]; exists {
			continue
		}
		seen[raw] = struct{}{}

		model := radarLMArenaCatalogModel{
			display:   display,
			raw:       raw,
			canonical: canonicalRadarLMArenaModelID(display),
		}
		if separator := strings.LastIndex(display, "/"); separator >= 0 && separator+1 < len(display) {
			model.namespaceCanonical = canonicalRadarLMArenaModelID(display[separator+1:])
		}
		if model.canonical == "" {
			continue
		}
		models = append(models, model)
	}

	sort.Slice(models, func(i, j int) bool {
		leftLower := strings.ToLower(models[i].display)
		rightLower := strings.ToLower(models[j].display)
		if leftLower != rightLower {
			return leftLower < rightLower
		}
		return models[i].display < models[j].display
	})
	return models
}

func matchRadarLMArenaCatalogModel(arenaModel string, catalog []radarLMArenaCatalogModel) (string, bool) {
	arenaRaw := strings.ToLower(strings.TrimSpace(arenaModel))
	arenaCanonical := canonicalRadarLMArenaModelID(arenaModel)
	if arenaCanonical == "" {
		return "", false
	}
	releaseAlias := radarLMArenaReleaseAlias(arenaCanonical)

	bestStrength := math.MaxInt
	best := make(map[string]string)
	for _, model := range catalog {
		strength := radarLMArenaNoMatch
		switch {
		case arenaRaw == model.raw:
			strength = radarLMArenaRawExact
		case arenaCanonical == model.canonical:
			strength = radarLMArenaSeparatorExact
		case model.namespaceCanonical != "" && arenaCanonical == model.namespaceCanonical:
			strength = radarLMArenaNamespaceAlias
		case releaseAlias != "" && releaseAlias == model.canonical:
			strength = radarLMArenaReleaseAliasMatch
		case releaseAlias != "" && model.namespaceCanonical != "" && releaseAlias == model.namespaceCanonical:
			strength = radarLMArenaNamespacedReleaseAlias
		}
		if strength == radarLMArenaNoMatch {
			continue
		}
		if strength > bestStrength {
			continue
		}
		if strength < bestStrength {
			bestStrength = strength
			clear(best)
		}
		best[model.raw] = model.display
	}
	if bestStrength == radarLMArenaNoMatch || len(best) != 1 {
		return "", false
	}
	for _, display := range best {
		return display, true
	}
	return "", false
}

const (
	radarLMArenaRawExact = iota
	radarLMArenaSeparatorExact
	radarLMArenaNamespaceAlias
	radarLMArenaReleaseAliasMatch
	radarLMArenaNamespacedReleaseAlias
	radarLMArenaNoMatch = math.MaxInt
)

func canonicalRadarLMArenaModelID(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	var builder strings.Builder
	builder.Grow(len(value))
	lastWasSeparator := true
	for _, r := range value {
		if r == '.' || r == '_' || r == '-' || unicode.IsSpace(r) {
			if !lastWasSeparator {
				_ = builder.WriteByte('-')
				lastWasSeparator = true
			}
			continue
		}
		_, _ = builder.WriteRune(r)
		lastWasSeparator = false
	}
	return strings.TrimSuffix(builder.String(), "-")
}

func radarLMArenaReleaseAlias(canonical string) string {
	const dashedDateLength = len("2006-01-02")
	if len(canonical) > dashedDateLength && canonical[len(canonical)-dashedDateLength-1] == '-' {
		suffix := canonical[len(canonical)-dashedDateLength:]
		if parsed, err := time.Parse("2006-01-02", suffix); err == nil && parsed.Format("2006-01-02") == suffix {
			return canonical[:len(canonical)-dashedDateLength-1]
		}
	}
	const compactDateLength = len("20060102")
	if len(canonical) > compactDateLength && canonical[len(canonical)-compactDateLength-1] == '-' {
		suffix := canonical[len(canonical)-compactDateLength:]
		if parsed, err := time.Parse("20060102", suffix); err == nil && parsed.Format("20060102") == suffix {
			return canonical[:len(canonical)-compactDateLength-1]
		}
	}
	return ""
}

func lessRadarLMArenaCatalogMatch(left, right radarLMArenaCatalogMatch) bool {
	if left.entry.Rank != right.entry.Rank {
		return left.entry.Rank < right.entry.Rank
	}
	if comparison := compareRadarLMArenaOptionalFloatDescending(left.entry.Elo, right.entry.Elo); comparison != 0 {
		return comparison < 0
	}
	leftArena := strings.ToLower(strings.TrimSpace(left.arenaModel))
	rightArena := strings.ToLower(strings.TrimSpace(right.arenaModel))
	if leftArena != rightArena {
		return leftArena < rightArena
	}
	if left.arenaModel != right.arenaModel {
		return left.arenaModel < right.arenaModel
	}
	leftCatalog := strings.ToLower(left.catalogID)
	rightCatalog := strings.ToLower(right.catalogID)
	if leftCatalog != rightCatalog {
		return leftCatalog < rightCatalog
	}
	if left.catalogID != right.catalogID {
		return left.catalogID < right.catalogID
	}
	if comparison := compareRadarLMArenaOptionalFloatDescending(left.entry.CILower, right.entry.CILower); comparison != 0 {
		return comparison < 0
	}
	if comparison := compareRadarLMArenaOptionalFloatDescending(left.entry.CIUpper, right.entry.CIUpper); comparison != 0 {
		return comparison < 0
	}
	if comparison := compareRadarLMArenaOptionalInt64Descending(left.entry.Votes, right.entry.Votes); comparison != 0 {
		return comparison < 0
	}
	return radarLMArenaOptionalString(left.entry.Vendor) < radarLMArenaOptionalString(right.entry.Vendor)
}

func compareRadarLMArenaOptionalFloatDescending(left, right *float64) int {
	if left == nil && right == nil {
		return 0
	}
	if left == nil {
		return 1
	}
	if right == nil {
		return -1
	}
	if *left > *right {
		return -1
	}
	if *left < *right {
		return 1
	}
	return 0
}

func compareRadarLMArenaOptionalInt64Descending(left, right *int64) int {
	if left == nil && right == nil {
		return 0
	}
	if left == nil {
		return 1
	}
	if right == nil {
		return -1
	}
	if *left > *right {
		return -1
	}
	if *left < *right {
		return 1
	}
	return 0
}

func radarLMArenaOptionalString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func cloneRadarLMArenaCatalogEntry(input LMArenaEntryDTO) LMArenaEntryDTO {
	return LMArenaEntryDTO{
		Rank:    input.Rank,
		Model:   input.Model,
		Vendor:  cloneRadarLMArenaCatalogString(input.Vendor),
		Elo:     cloneRadarLMArenaCatalogFloat64(input.Elo),
		CILower: cloneRadarLMArenaCatalogFloat64(input.CILower),
		CIUpper: cloneRadarLMArenaCatalogFloat64(input.CIUpper),
		Votes:   cloneRadarLMArenaCatalogInt64(input.Votes),
	}
}

func cloneRadarLMArenaCatalogString(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneRadarLMArenaCatalogFloat64(value *float64) *float64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneRadarLMArenaCatalogInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneRadarLMArenaCatalogTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func radarLMArenaCatalogVoteTotal(entries []LMArenaEntryDTO) *int64 {
	var total int64
	for _, entry := range entries {
		if entry.Votes == nil || *entry.Votes < 0 || total > math.MaxInt64-*entry.Votes {
			return nil
		}
		total += *entry.Votes
	}
	return &total
}
