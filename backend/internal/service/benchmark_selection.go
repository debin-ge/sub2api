package service

import "sort"

// SelectBenchmarkTasks returns the fixed task set for a run: all enabled tasks
// ordered by sort_order then id, or the first taskCount of them when taskCount
// is positive. There is no sampling — the task set is fixed so trends stay
// comparable across runs.
func SelectBenchmarkTasks(tasks []BenchmarkTaskCandidate, taskCount int) []BenchmarkTaskCandidate {
	enabled := make([]BenchmarkTaskCandidate, 0, len(tasks))
	for _, task := range tasks {
		if task.Enabled {
			enabled = append(enabled, task)
		}
	}
	sort.SliceStable(enabled, func(i, j int) bool {
		if enabled[i].SortOrder != enabled[j].SortOrder {
			return enabled[i].SortOrder < enabled[j].SortOrder
		}
		return enabled[i].ID < enabled[j].ID
	})
	if taskCount > 0 && taskCount < len(enabled) {
		enabled = enabled[:taskCount]
	}
	return enabled
}
