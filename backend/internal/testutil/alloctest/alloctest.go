// Package alloctest measures allocations in tests that share a process with
// other goroutines.
package alloctest

import (
	"math"
	"testing"
)

// MinPerRun returns the lowest of up to trials testing.AllocsPerRun(runs, f)
// measurements, stopping early once one reports zero.
//
// AllocsPerRun reads the process-wide malloc counter, so any goroutine that
// runs during a measurement adds to the count. In a large test package that
// includes background workers started by earlier tests, which made fixed
// allocation bounds fail at random on CI. That noise only ever adds, so the
// minimum over several short trials is the measured function's own count.
func MinPerRun(trials, runs int, f func()) float64 {
	best := math.Inf(1)
	for i := 0; i < trials; i++ {
		if allocs := testing.AllocsPerRun(runs, f); allocs < best {
			best = allocs
		}
		if best == 0 {
			break
		}
	}
	return best
}
