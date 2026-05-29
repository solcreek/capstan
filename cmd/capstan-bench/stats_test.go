package main

import (
	"math"
	"testing"
)

func TestPercentileExact(t *testing.T) {
	// 1..100 sorted — well-known percentile reference values.
	xs := make([]float64, 100)
	for i := range xs {
		xs[i] = float64(i + 1)
	}
	// numpy linear interpolation: p50 of 1..100 = 50.5, p95 = 95.05, p99 = 99.01
	tests := []struct {
		q    float64
		want float64
	}{
		{0.50, 50.5},
		{0.95, 95.05},
		{0.99, 99.01},
	}
	for _, tc := range tests {
		got := percentile(xs, tc.q)
		if math.Abs(got-tc.want) > 0.01 {
			t.Errorf("percentile(1..100, %v) = %v, want %v", tc.q, got, tc.want)
		}
	}
}

func TestPercentileSingleSample(t *testing.T) {
	if got := percentile([]float64{42}, 0.5); got != 42 {
		t.Errorf("single-sample p50 = %v, want 42", got)
	}
}

func TestPercentileEmpty(t *testing.T) {
	if got := percentile(nil, 0.5); got != 0 {
		t.Errorf("empty p50 = %v, want 0", got)
	}
}

func TestStatsAddAndSummary(t *testing.T) {
	s := &stats{name: "op"}
	for _, v := range []float64{10, 20, 30, 40, 50} {
		s.add(v, true)
	}
	s.add(0, false) // error
	s.add(0, false)

	sum := s.summary()
	if sum.N != 5 {
		t.Errorf("N = %d, want 5", sum.N)
	}
	if sum.Errs != 2 {
		t.Errorf("Errs = %d, want 2", sum.Errs)
	}
	if sum.Min != 10 || sum.Max != 50 {
		t.Errorf("Min/Max = %v/%v, want 10/50", sum.Min, sum.Max)
	}
	if sum.Mean != 30 {
		t.Errorf("Mean = %v, want 30", sum.Mean)
	}
}

func TestStatsAllErrors(t *testing.T) {
	s := &stats{name: "op"}
	s.add(0, false)
	s.add(0, false)
	sum := s.summary()
	if sum.N != 0 || sum.Errs != 2 {
		t.Errorf("N=%d errs=%d, want 0/2", sum.N, sum.Errs)
	}
}
