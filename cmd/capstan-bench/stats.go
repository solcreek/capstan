// stats.go — small percentile helper for the bench harness.
//
// Bench has tens to a few hundred samples per operation; sorting in memory
// and indexing is plenty for that scale. No streaming quantile estimator
// needed (and we want exact percentiles for small-N reports).

package main

import (
	"fmt"
	"math"
	"sort"
)

type stats struct {
	name    string
	ok      int
	errCnt  int
	samples []float64 // milliseconds; only successful samples
}

func (s *stats) add(latencyMs float64, ok bool) {
	if ok {
		s.ok++
		s.samples = append(s.samples, latencyMs)
	} else {
		s.errCnt++
	}
}

// summary holds the printable form of one operation's measurements.
type summary struct {
	N    int
	Errs int
	P50  float64
	P95  float64
	P99  float64
	Min  float64
	Max  float64
	Mean float64
}

func (s *stats) summary() summary {
	if len(s.samples) == 0 {
		return summary{N: s.ok, Errs: s.errCnt}
	}
	sorted := append([]float64(nil), s.samples...)
	sort.Float64s(sorted)

	sum := 0.0
	for _, v := range sorted {
		sum += v
	}
	mean := sum / float64(len(sorted))

	return summary{
		N:    s.ok,
		Errs: s.errCnt,
		P50:  percentile(sorted, 0.50),
		P95:  percentile(sorted, 0.95),
		P99:  percentile(sorted, 0.99),
		Min:  sorted[0],
		Max:  sorted[len(sorted)-1],
		Mean: mean,
	}
}

// percentile returns the q-th percentile of a sorted ascending slice using
// linear interpolation between closest ranks (the same method numpy and
// most stat libraries use by default).
func percentile(sorted []float64, q float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return sorted[0]
	}
	pos := q * float64(len(sorted)-1)
	lo := int(math.Floor(pos))
	hi := int(math.Ceil(pos))
	if lo == hi {
		return sorted[lo]
	}
	frac := pos - float64(lo)
	return sorted[lo] + frac*(sorted[hi]-sorted[lo])
}

func (s summary) String() string {
	return fmt.Sprintf("n=%d err=%d p50=%.1f p95=%.1f p99=%.1f min=%.1f max=%.1f mean=%.1f",
		s.N, s.Errs, s.P50, s.P95, s.P99, s.Min, s.Max, s.Mean)
}
