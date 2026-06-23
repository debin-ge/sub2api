package service

import "testing"

func TestBenchmarkConstants(t *testing.T) {
	t.Parallel()

	if BenchmarkTaskScaleSmall != "small" {
		t.Fatalf("small scale = %q", BenchmarkTaskScaleSmall)
	}
	if BenchmarkTaskScaleMedium != "medium" {
		t.Fatalf("medium scale = %q", BenchmarkTaskScaleMedium)
	}
	if BenchmarkTaskScaleFull != "full" {
		t.Fatalf("full scale = %q", BenchmarkTaskScaleFull)
	}
	if BenchmarkTaskScaleCustom != "custom" {
		t.Fatalf("custom scale = %q", BenchmarkTaskScaleCustom)
	}
	if BenchmarkConfidenceLow != "low" || BenchmarkConfidenceMedium != "medium" || BenchmarkConfidenceHigh != "high" {
		t.Fatalf("unexpected confidence constants")
	}
	if BenchmarkResultStatusScored != "scored" || BenchmarkResultStatusTimeout != "timeout" {
		t.Fatalf("unexpected result status constants")
	}
}
