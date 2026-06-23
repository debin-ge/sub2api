package service

import (
	"errors"
	"math/rand"
	"sort"
)

func SelectBenchmarkTasks(tasks []BenchmarkTaskCandidate, cfg BenchmarkSelectionConfig) ([]BenchmarkTaskCandidate, error) {
	if cfg.TaskScale == "" {
		cfg.TaskScale = BenchmarkTaskScaleMedium
	}
	if !isValidBenchmarkTaskScale(cfg.TaskScale) {
		return nil, errors.New("invalid benchmark task scale")
	}

	typeSet := stringSet(cfg.TaskTypes)
	diffSet := stringSet(cfg.DifficultyFilter)
	tagSet := stringSet(cfg.TagFilter)
	filtered := make([]BenchmarkTaskCandidate, 0, len(tasks))
	for _, task := range tasks {
		if !task.Enabled {
			continue
		}
		if len(typeSet) > 0 && !typeSet[task.Type] {
			continue
		}
		if len(diffSet) > 0 && !diffSet[task.Difficulty] {
			continue
		}
		if len(tagSet) > 0 && !hasAnyTag(task.Tags, tagSet) {
			continue
		}
		if !scaleAllowsTask(cfg.TaskScale, task.MinScale) {
			continue
		}
		filtered = append(filtered, task)
	}
	sort.Slice(filtered, func(i, j int) bool { return filtered[i].ID < filtered[j].ID })
	if cfg.TaskScale == BenchmarkTaskScaleFull && len(cfg.PerTypeLimit) == 0 {
		return filtered, nil
	}

	limit := cfg.TaskCountLimit
	if limit <= 0 {
		switch cfg.TaskScale {
		case BenchmarkTaskScaleSmall:
			limit = 20
		case BenchmarkTaskScaleMedium:
			limit = 100
		default:
			limit = len(filtered)
		}
	}
	if limit >= len(filtered) && len(cfg.PerTypeLimit) == 0 {
		return filtered, nil
	}

	grouped := map[string][]BenchmarkTaskCandidate{}
	for _, task := range filtered {
		grouped[task.Type] = append(grouped[task.Type], task)
	}
	types := make([]string, 0, len(grouped))
	for typ := range grouped {
		types = append(types, typ)
	}
	sort.Strings(types)
	rng := rand.New(rand.NewSource(cfg.SelectionSeed))
	for _, typ := range types {
		rng.Shuffle(len(grouped[typ]), func(i, j int) {
			grouped[typ][i], grouped[typ][j] = grouped[typ][j], grouped[typ][i]
		})
	}

	selected := make([]BenchmarkTaskCandidate, 0, limit)
	selectedByType := map[string]int{}
	for len(selected) < limit {
		added := false
		for _, typ := range types {
			if len(selected) >= limit {
				break
			}
			if len(grouped[typ]) == 0 {
				continue
			}
			if cap, ok := cfg.PerTypeLimit[typ]; ok {
				if cap <= 0 || selectedByType[typ] >= cap {
					continue
				}
			}
			selected = append(selected, grouped[typ][0])
			grouped[typ] = grouped[typ][1:]
			selectedByType[typ]++
			added = true
		}
		if !added {
			break
		}
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].ID < selected[j].ID })
	return selected, nil
}

func stringSet(values []string) map[string]bool {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]bool, len(values))
	for _, value := range values {
		if value != "" {
			out[value] = true
		}
	}
	return out
}

func hasAnyTag(tags []string, allowed map[string]bool) bool {
	for _, tag := range tags {
		if allowed[tag] {
			return true
		}
	}
	return false
}

func scaleAllowsTask(scale string, minScale string) bool {
	if minScale == "" {
		return true
	}
	order := map[string]int{
		BenchmarkTaskScaleSmall:  1,
		BenchmarkTaskScaleMedium: 2,
		BenchmarkTaskScaleFull:   3,
		BenchmarkTaskScaleCustom: 3,
	}
	return order[scale] >= order[minScale]
}
