package domain

import (
	"math"
	"testing"
)

func pf(v float64) *float64 { return &v }

func TestShelfLifeDecreasingAgainstLowerLimit(t *testing.T) {
	// Exactly linear: 100 at day 0, losing 1 per 30 days; the limit is 90.
	pts := []StabilityPoint{{0, 100}, {30, 99}, {60, 98}, {90, 97}, {180, 94}}
	r := EstimateShelfLife(pts, pf(90), nil)
	if r.Side != "lower" || math.Abs(r.Slope+1.0/30) > 1e-4 || r.R2 < 0.999 {
		t.Fatalf("fit: %+v", r)
	}
	// With no scatter the bound is the line: 300 days.
	if r.Days == nil || math.Abs(*r.Days-299) > 1 {
		t.Fatalf("perfect data reaches the limit at day 300: %+v", r.Days)
	}
}

func TestShelfLifeScatterShortensIt(t *testing.T) {
	// Both reach 96 inside the horizon (twice the 90 days of data).
	clean := EstimateShelfLife([]StabilityPoint{{0, 100}, {30, 99}, {60, 98}, {90, 97}}, pf(96), nil)
	noisy := EstimateShelfLife([]StabilityPoint{{0, 100.6}, {30, 98.4}, {60, 98.7}, {90, 96.4}}, pf(96), nil)
	if clean.Days == nil || noisy.Days == nil || *noisy.Days >= *clean.Days {
		t.Fatalf("scatter should cost shelf life: clean %v noisy %v", clean.Days, noisy.Days)
	}
}

func TestShelfLifeUpperLimitAndEdges(t *testing.T) {
	// Moisture creeping up towards 12.5.
	up := EstimateShelfLife([]StabilityPoint{{0, 10.0}, {30, 10.4}, {60, 10.9}, {90, 11.3}}, nil, pf(12.5))
	if up.Side != "upper" || up.Days == nil || *up.Days < 120 || *up.Days > 180 {
		t.Fatalf("moisture: %+v", up)
	}
	// Improving with only a lower limit: nothing to cross.
	if r := EstimateShelfLife([]StabilityPoint{{0, 1}, {30, 2}, {60, 3}}, pf(0.5), nil); r.Days != nil || r.Limit != nil {
		t.Fatalf("no limit on the trend's side: %+v", r)
	}
	if r := EstimateShelfLife([]StabilityPoint{{0, 1}, {30, 2}}, pf(0.5), nil); r.Days != nil || r.Supported == "" {
		t.Fatalf("two points: %+v", r)
	}
	if r := EstimateShelfLife([]StabilityPoint{{30, 1}, {30, 2}, {30, 3}}, pf(0.5), nil); r.Days != nil {
		t.Fatalf("one age: %+v", r)
	}
	// Barely moving: supported to the horizon, not beyond.
	flat := EstimateShelfLife([]StabilityPoint{{0, 100}, {30, 100}, {60, 99.99}, {90, 100}}, pf(90), nil)
	if flat.Days != nil || len(flat.Projection) == 0 || flat.Projection[len(flat.Projection)-1].Day > 180.01 {
		t.Fatalf("flat: %+v", flat)
	}
}
