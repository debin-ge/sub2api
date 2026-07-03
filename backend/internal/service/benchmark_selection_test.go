package service

import (
	"testing"
)

func TestSelectBenchmarkTasksAllEnabled(t *testing.T) {
	tasks := []BenchmarkTaskCandidate{
		{ID: 3, Type: "coding", SortOrder: 2, Enabled: true},
		{ID: 1, Type: "reasoning", SortOrder: 1, Enabled: true},
		{ID: 2, Type: "math", SortOrder: 1, Enabled: false},
		{ID: 4, Type: "writing", SortOrder: 3, Enabled: true},
	}

	got := SelectBenchmarkTasks(tasks, 0)
	if len(got) != 3 {
		t.Fatalf("expected 3 enabled tasks, got %d", len(got))
	}
	// ordered by sort_order then id: id1(s1), id3(s2), id4(s3)
	wantIDs := []int64{1, 3, 4}
	for i, task := range got {
		if task.ID != wantIDs[i] {
			t.Fatalf("position %d: want id %d, got %d", i, wantIDs[i], task.ID)
		}
	}
}

func TestSelectBenchmarkTasksFirstN(t *testing.T) {
	tasks := []BenchmarkTaskCandidate{
		{ID: 1, SortOrder: 1, Enabled: true},
		{ID: 2, SortOrder: 2, Enabled: true},
		{ID: 3, SortOrder: 3, Enabled: true},
	}
	got := SelectBenchmarkTasks(tasks, 2)
	if len(got) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(got))
	}
	if got[0].ID != 1 || got[1].ID != 2 {
		t.Fatalf("expected first 2 by order, got %v %v", got[0].ID, got[1].ID)
	}
}

func TestSelectBenchmarkTasksCountExceedsAvailable(t *testing.T) {
	tasks := []BenchmarkTaskCandidate{
		{ID: 1, Enabled: true},
		{ID: 2, Enabled: true},
	}
	got := SelectBenchmarkTasks(tasks, 10)
	if len(got) != 2 {
		t.Fatalf("expected all available when count exceeds, got %d", len(got))
	}
}
