package service

import "testing"

func TestSelectBenchmarkTasksFiltersByTypeDifficultyAndTags(t *testing.T) {
	t.Parallel()

	tasks := []BenchmarkTaskCandidate{
		{ID: 1, Type: "math_reasoning", Difficulty: "easy", Tags: []string{"public"}, Weight: 1, MinScale: BenchmarkTaskScaleSmall, Enabled: true},
		{ID: 2, Type: "coding", Difficulty: "hard", Tags: []string{"private"}, Weight: 1, MinScale: BenchmarkTaskScaleMedium, Enabled: true},
		{ID: 3, Type: "math_reasoning", Difficulty: "hard", Tags: []string{"public"}, Weight: 1, MinScale: BenchmarkTaskScaleSmall, Enabled: false},
	}
	got, err := SelectBenchmarkTasks(tasks, BenchmarkSelectionConfig{
		TaskTypes:        []string{"math_reasoning"},
		TaskScale:        BenchmarkTaskScaleSmall,
		DifficultyFilter: []string{"easy"},
		TagFilter:        []string{"public"},
		SelectionSeed:    42,
	})
	if err != nil {
		t.Fatalf("SelectBenchmarkTasks error = %v", err)
	}
	if len(got) != 1 || got[0].ID != 1 {
		t.Fatalf("selected tasks = %#v", got)
	}
}

func TestSelectBenchmarkTasksCustomLimitIsStable(t *testing.T) {
	t.Parallel()

	tasks := []BenchmarkTaskCandidate{
		{ID: 1, Type: "math_reasoning", Difficulty: "easy", Weight: 1, MinScale: BenchmarkTaskScaleSmall, Enabled: true},
		{ID: 2, Type: "math_reasoning", Difficulty: "medium", Weight: 1, MinScale: BenchmarkTaskScaleSmall, Enabled: true},
		{ID: 3, Type: "coding", Difficulty: "hard", Weight: 1, MinScale: BenchmarkTaskScaleSmall, Enabled: true},
		{ID: 4, Type: "coding", Difficulty: "easy", Weight: 1, MinScale: BenchmarkTaskScaleSmall, Enabled: true},
	}
	cfg := BenchmarkSelectionConfig{
		TaskTypes:      []string{"math_reasoning", "coding"},
		TaskScale:      BenchmarkTaskScaleCustom,
		TaskCountLimit: 2,
		SelectionSeed:  20260623,
	}
	first, err := SelectBenchmarkTasks(tasks, cfg)
	if err != nil {
		t.Fatalf("first selection error = %v", err)
	}
	second, err := SelectBenchmarkTasks(tasks, cfg)
	if err != nil {
		t.Fatalf("second selection error = %v", err)
	}
	if len(first) != 2 || len(second) != 2 {
		t.Fatalf("selection lengths = %d and %d", len(first), len(second))
	}
	for i := range first {
		if first[i].ID != second[i].ID {
			t.Fatalf("selection not stable: %#v vs %#v", first, second)
		}
	}
}
