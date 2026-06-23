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

func TestSelectBenchmarkTasksRespectsPerTypeLimit(t *testing.T) {
	t.Parallel()

	tasks := []BenchmarkTaskCandidate{
		{ID: 1, Type: "math_reasoning", Difficulty: "easy", Weight: 1, MinScale: BenchmarkTaskScaleSmall, Enabled: true},
		{ID: 2, Type: "math_reasoning", Difficulty: "medium", Weight: 1, MinScale: BenchmarkTaskScaleSmall, Enabled: true},
		{ID: 3, Type: "math_reasoning", Difficulty: "hard", Weight: 1, MinScale: BenchmarkTaskScaleSmall, Enabled: true},
		{ID: 4, Type: "coding", Difficulty: "easy", Weight: 1, MinScale: BenchmarkTaskScaleSmall, Enabled: true},
		{ID: 5, Type: "coding", Difficulty: "medium", Weight: 1, MinScale: BenchmarkTaskScaleSmall, Enabled: true},
		{ID: 6, Type: "knowledge", Difficulty: "easy", Weight: 1, MinScale: BenchmarkTaskScaleSmall, Enabled: true},
		{ID: 7, Type: "knowledge", Difficulty: "medium", Weight: 1, MinScale: BenchmarkTaskScaleSmall, Enabled: true},
	}
	got, err := SelectBenchmarkTasks(tasks, BenchmarkSelectionConfig{
		TaskTypes:      []string{"math_reasoning", "coding", "knowledge"},
		TaskScale:      BenchmarkTaskScaleCustom,
		TaskCountLimit: 6,
		PerTypeLimit: map[string]int{
			"math_reasoning": 1,
			"coding":         0,
			"unknown":        -1,
		},
		SelectionSeed: 20260623,
	})
	if err != nil {
		t.Fatalf("SelectBenchmarkTasks error = %v", err)
	}
	counts := map[string]int{}
	for _, task := range got {
		counts[task.Type]++
		if task.Type == "coding" {
			t.Fatalf("selected type with zero per-type limit: %#v", got)
		}
	}
	if counts["math_reasoning"] != 1 {
		t.Fatalf("math_reasoning selections = %d, want 1; selected tasks = %#v", counts["math_reasoning"], got)
	}
	if counts["knowledge"] != 2 {
		t.Fatalf("knowledge selections = %d, want 2; selected tasks = %#v", counts["knowledge"], got)
	}
	if len(got) != 3 {
		t.Fatalf("selected task count = %d, want 3; selected tasks = %#v", len(got), got)
	}
}
