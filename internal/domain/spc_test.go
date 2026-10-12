package domain

import (
	"math"
	"testing"
)

func near(a, b float64) bool { return math.Abs(a-b) < 0.01 }

func TestIMRUsesTheMovingRange(t *testing.T) {
	// Moving ranges 1,1,1,1 → MR̄ 1 → limits mean ± 2.66.
	ind, mr, mrl := IMR([]float64{10, 11, 10, 11, 10})
	if !near(ind.Center, 10.4) || !near(ind.UCL, 13.06) || !near(ind.LCL, 7.74) || len(mr) != 4 {
		t.Fatalf("I chart: %+v mr %v", ind, mr)
	}
	if !near(mrl.UCL, 3.267) || !near(ind.Sigma, 0.8865) {
		t.Fatalf("MR chart: %+v sigma %v", mrl, ind.Sigma)
	}
}

func TestXBarR(t *testing.T) {
	groups := []Subgroup{
		{Key: "A", Values: []float64{10, 12, 11}},
		{Key: "B", Values: []float64{11, 13, 12}},
		{Key: "C", Values: []float64{9}},
	}
	out, grand, dropped := XBar(groups, false)
	if dropped != 1 || len(out) != 2 || !near(grand, 11.5) {
		t.Fatalf("xbar: %+v grand %v dropped %d", out, grand, dropped)
	}
	// R̄ 2, n 3: A2 1.023 → ±2.046; D4 2.574 → 5.148.
	if !near(out[0].Limits.UCL, 13.546) || !near(out[0].SpreadLimits.UCL, 5.148) {
		t.Fatalf("limits: %+v %+v", out[0].Limits, out[0].SpreadLimits)
	}
}

func TestConstantsBeyondTheTable(t *testing.T) {
	c, ok := Constants(25)
	if !ok || !near(c.A3, 0.606) || !near(c.B4, 1.435) {
		t.Fatalf("n=25: %+v", c)
	}
	if _, ok := Constants(1); ok {
		t.Fatal("n=1 has no constants")
	}
}

func TestPChartLimitsWidenForSmallSubgroups(t *testing.T) {
	out := PChart([]Proportion{{Key: "d1", Inspected: 100, Defective: 5}, {Key: "d2", Inspected: 10, Defective: 1}, {Key: "d3", Inspected: 0}})
	if len(out) != 2 || !near(out[0].Limits.Center, 0.0545) || out[1].Limits.UCL <= out[0].Limits.UCL || out[0].Limits.LCL != 0 {
		t.Fatalf("p chart: %+v", out)
	}
}

func TestCChart(t *testing.T) {
	l := CChart([]float64{4, 4, 4, 4})
	if l.Center != 4 || l.UCL != 10 || l.LCL != 0 {
		t.Fatalf("c chart: %+v", l)
	}
}

func TestRules(t *testing.T) {
	band := []Limits{{Center: 0, UCL: 3, LCL: -3, Sigma: 1}}
	has := func(got []int, r int) bool {
		for _, g := range got {
			if g == r {
				return true
			}
		}
		return false
	}
	// Rule 1 at the point beyond 3σ.
	if r := ApplyRules([]float64{0, 3.5}, band, DefaultRules); !has(r[1], 1) || len(r[0]) != 0 {
		t.Fatalf("rule 1: %v", r)
	}
	// Rule 2 completes on the ninth point above the centre, not before.
	nine := []float64{0.5, 0.5, 0.5, 0.5, 0.5, 0.5, 0.5, 0.5, 0.5}
	r := ApplyRules(nine, band, DefaultRules)
	if !has(r[8], 2) || has(r[7], 2) {
		t.Fatalf("rule 2: %v", r)
	}
	// Rule 3: six rising.
	if r := ApplyRules([]float64{-1, -0.6, -0.2, 0.1, 0.4, 0.8}, band, DefaultRules); !has(r[5], 3) {
		t.Fatalf("rule 3: %v", r)
	}
	// Rule 5: two of three beyond 2σ, same side.
	if r := ApplyRules([]float64{2.5, 0, 2.2}, band, DefaultRules); !has(r[2], 5) {
		t.Fatalf("rule 5: %v", r)
	}
	// Rule 6: four of five beyond 1σ.
	if r := ApplyRules([]float64{1.5, 1.2, 0, 1.4, 1.1}, band, DefaultRules); !has(r[4], 6) {
		t.Fatalf("rule 6: %v", r)
	}
	// Disabled rules are not reported.
	if r := ApplyRules([]float64{0, 3.5}, band, []int{2}); len(r[1]) != 0 {
		t.Fatalf("disabled: %v", r)
	}
	// A quiet process breaks nothing.
	for i, p := range ApplyRules([]float64{0.2, -0.3, 0.5, -0.1, 0.4, -0.6, 0.1}, band, DefaultRules) {
		if len(p) != 0 {
			t.Fatalf("quiet point %d broke %v", i, p)
		}
	}
}
