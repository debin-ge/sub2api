package repository

import (
	"reflect"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
)

func TestBenchmarkTasksOrderedByTitleReturnsFirstRowPerRequestedTitle(t *testing.T) {
	rows := []*dbent.BenchmarkTask{
		{ID: 10, Title: "Beta", SortOrder: 10},
		{ID: 11, Title: "Alpha", SortOrder: 20},
		{ID: 12, Title: "Beta", SortOrder: 30},
		{ID: 13, Title: "Gamma", SortOrder: 40},
	}

	ordered := benchmarkTasksOrderedByTitle(rows, []string{"Alpha", "Beta", "Missing", "Beta"})

	got := make([]int64, 0, len(ordered))
	for _, task := range ordered {
		got = append(got, task.ID)
	}
	want := []int64{11, 10}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ordered task ids = %#v, want %#v", got, want)
	}
}
