package domain

import (
	"math"
)

/*
Shelf-life estimate from stability data, after ICH Q1E: fit the attribute
against time by least squares, and take the shelf life as the earliest time at
which the one-sided 95 % confidence bound for the mean crosses the
acceptance limit — the lower bound against a lower limit, the upper bound
against an upper one. The bound, not the fitted line, is what is compared:
with three noisy points the line can sit comfortably inside the limit while
the data cannot support it.
*/

// t95 is Student's t, one-sided 95 %, by degrees of freedom.
var t95 = []float64{0, 6.314, 2.920, 2.353, 2.132, 2.015, 1.943, 1.895, 1.860, 1.833, 1.812,
	1.796, 1.782, 1.771, 1.761, 1.753, 1.746, 1.740, 1.734, 1.729, 1.725,
	1.721, 1.717, 1.714, 1.711, 1.708, 1.706, 1.703, 1.701, 1.699, 1.697}

func tOneSided95(df int) float64 {
	if df < 1 {
		return math.NaN()
	}
	if df < len(t95) {
		return t95[df]
	}
	return 1.645
}

// StabilityPoint is one result at one age.
type StabilityPoint struct {
	Day   float64 `json:"day"`
	Value float64 `json:"value"`
}

// ProjectionPoint is the fitted line and its bound at one age.
type ProjectionPoint struct {
	Day   float64 `json:"day"`
	Fit   float64 `json:"fit"`
	Bound float64 `json:"bound"`
}

// ShelfLife is the fit and what it supports.
type ShelfLife struct {
	N         int      `json:"n"`
	Slope     float64  `json:"slope_per_day"`
	Intercept float64  `json:"intercept"`
	R2        float64  `json:"r2"`
	Limit     *float64 `json:"limit,omitempty"`
	Side      string   `json:"side,omitempty"` // "lower" or "upper"
	// Days is the supported shelf life; nil when the bound never crosses the
	// limit within the horizon (Supported then says so) or there is too
	// little data.
	Days       *float64          `json:"shelf_life_days,omitempty"`
	Supported  string            `json:"supported"`
	Projection []ProjectionPoint `json:"projection"`
}

const minStabilityPoints = 3

/*
EstimateShelfLife fits points and reads the shelf life against lsl / usl. The
side is the one the trend heads towards; with no trend, or no limit on that
side, there is nothing to cross and the estimate is the whole horizon. The
horizon is how far the projection is drawn (and searched), capped so a nearly
flat trend is not extrapolated for decades: ICH Q1E allows extrapolation to
at most twice the period covered by data.
*/
func EstimateShelfLife(points []StabilityPoint, lsl, usl *float64) ShelfLife {
	out := ShelfLife{N: len(points), Projection: []ProjectionPoint{}}
	if len(points) < minStabilityPoints {
		out.Supported = "needs at least 3 results"
		return out
	}
	var sx, sy, maxDay float64
	for _, p := range points {
		sx += p.Day
		sy += p.Value
		maxDay = math.Max(maxDay, p.Day)
	}
	n := float64(len(points))
	mx, my := sx/n, sy/n
	var sxx, sxy, syy float64
	for _, p := range points {
		sxx += (p.Day - mx) * (p.Day - mx)
		sxy += (p.Day - mx) * (p.Value - my)
		syy += (p.Value - my) * (p.Value - my)
	}
	if sxx == 0 {
		out.Supported = "all results are at one age; a trend needs several"
		return out
	}
	b := sxy / sxx
	a := my - b*mx
	var sse float64
	for _, p := range points {
		r := p.Value - (a + b*p.Day)
		sse += r * r
	}
	s := math.Sqrt(sse / (n - 2))
	tv := tOneSided95(len(points) - 2)
	out.Slope, out.Intercept = round4(b), round4(a)
	if syy > 0 {
		out.R2 = round4(1 - sse/syy)
	}

	// The side the trend heads towards decides which limit and which bound.
	var limit *float64
	sign := 0.0
	switch {
	case b < 0 && lsl != nil:
		limit, sign, out.Side = lsl, -1, "lower"
	case b > 0 && usl != nil:
		limit, sign, out.Side = usl, 1, "upper"
	}
	out.Limit = limit
	bound := func(x float64) float64 {
		se := s * math.Sqrt(1/n+(x-mx)*(x-mx)/sxx)
		if sign == 0 {
			return a + b*x
		}
		return a + b*x + sign*tv*se
	}
	horizon := 2 * maxDay
	step := math.Max(1, horizon/200)
	for x := 0.0; x <= horizon+1e-9; x += step {
		out.Projection = append(out.Projection, ProjectionPoint{Day: round4(x), Fit: round4(a + b*x), Bound: round4(bound(x))})
	}
	if limit == nil {
		out.Supported = "no limit on the side the trend is heading; no expiry from these data"
		return out
	}
	crossed := func(x float64) bool {
		if sign < 0 {
			return bound(x) < *limit
		}
		return bound(x) > *limit
	}
	if crossed(0) {
		zero := 0.0
		out.Days = &zero
		out.Supported = "the confidence bound is outside the limit from the start"
		return out
	}
	if !crossed(horizon) {
		out.Supported = "supported to the end of the extrapolation horizon (twice the data); test longer to claim more"
		return out
	}
	lo, hi := 0.0, horizon
	for i := 0; i < 60; i++ {
		mid := (lo + hi) / 2
		if crossed(mid) {
			hi = mid
		} else {
			lo = mid
		}
	}
	days := math.Floor(lo)
	out.Days = &days
	out.Supported = "estimated where the one-sided 95% confidence bound meets the limit"
	return out
}

func round4(v float64) float64 { return math.Round(v*10000) / 10000 }
