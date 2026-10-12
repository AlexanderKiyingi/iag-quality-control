package domain

import "math"

/*
Control-chart maths (Montgomery, Introduction to Statistical Quality Control).

Limits come from the process's short-term variation — the moving range for
individuals, the subgroup ranges or standard deviations for averages — not
from the overall standard deviation, which a drifting process inflates until
its own drift sits inside the limits.
*/

// ChartConstants are the tabulated factors for a subgroup of size n.
type ChartConstants struct {
	A2, D3, D4, A3, B3, B4, D2, C4 float64
}

var constantTable = map[int]ChartConstants{
	2:  {1.880, 0, 3.267, 2.659, 0, 3.267, 1.128, 0.7979},
	3:  {1.023, 0, 2.574, 1.954, 0, 2.568, 1.693, 0.8862},
	4:  {0.729, 0, 2.282, 1.628, 0, 2.266, 2.059, 0.9213},
	5:  {0.577, 0, 2.114, 1.427, 0, 2.089, 2.326, 0.9400},
	6:  {0.483, 0, 2.004, 1.287, 0.030, 1.970, 2.534, 0.9515},
	7:  {0.419, 0.076, 1.924, 1.182, 0.118, 1.882, 2.704, 0.9594},
	8:  {0.373, 0.136, 1.864, 1.099, 0.185, 1.815, 2.847, 0.9650},
	9:  {0.337, 0.184, 1.816, 1.032, 0.239, 1.761, 2.970, 0.9693},
	10: {0.308, 0.223, 1.777, 0.975, 0.284, 1.716, 3.078, 0.9727},
}

// Constants returns the factors for n ≥ 2. Beyond the table the S-chart
// factors follow from c4; the R-chart ones are not meaningful there and are
// left zero (use X̄-S for large subgroups).
func Constants(n int) (ChartConstants, bool) {
	if n < 2 {
		return ChartConstants{}, false
	}
	if c, ok := constantTable[n]; ok {
		return c, true
	}
	fn := float64(n)
	c4 := 4 * (fn - 1) / (4*fn - 3)
	k := 3 * math.Sqrt(1-c4*c4) / c4
	return ChartConstants{A3: 3 / (c4 * math.Sqrt(fn)), B3: math.Max(0, 1-k), B4: 1 + k, C4: c4}, true
}

// Limits is one control band; Sigma is the 1σ width used by the zone rules.
type Limits struct {
	Center float64 `json:"center"`
	UCL    float64 `json:"ucl"`
	LCL    float64 `json:"lcl"`
	Sigma  float64 `json:"sigma"`
}

func r4(v float64) float64 { return math.Round(v*10000) / 10000 }

// IMR returns the individuals limits and the moving-range chart for values.
func IMR(values []float64) (ind Limits, mr []float64, mrLimits Limits) {
	if len(values) == 0 {
		return
	}
	mean := 0.0
	for _, v := range values {
		mean += v
	}
	mean /= float64(len(values))
	mr = make([]float64, 0, len(values))
	sum := 0.0
	for i := 1; i < len(values); i++ {
		d := math.Abs(values[i] - values[i-1])
		mr = append(mr, r4(d))
		sum += d
	}
	mrBar := 0.0
	if len(mr) > 0 {
		mrBar = sum / float64(len(mr))
	}
	sigma := mrBar / 1.128
	ind = Limits{Center: r4(mean), UCL: r4(mean + 2.66*mrBar), LCL: r4(mean - 2.66*mrBar), Sigma: r4(sigma)}
	mrLimits = Limits{Center: r4(mrBar), UCL: r4(3.267 * mrBar), LCL: 0, Sigma: r4(0.853 * sigma)}
	return
}

// Subgroup is a set of readings taken together (one batch, one sample, one day).
type Subgroup struct {
	Key    string    `json:"key"`
	Values []float64 `json:"-"`
	N      int       `json:"n"`
	Mean   float64   `json:"mean"`
	Range  float64   `json:"range"`
	StdDev float64   `json:"std_dev"`
	// Limits for this subgroup's mean (they widen for a smaller n).
	Limits Limits `json:"limits"`
	// Limits for its range or standard deviation.
	SpreadLimits Limits `json:"spread_limits"`
}

func describe(values []float64) (mean, rng, std float64) {
	lo, hi := values[0], values[0]
	for _, v := range values {
		mean += v
		lo, hi = math.Min(lo, v), math.Max(hi, v)
	}
	mean /= float64(len(values))
	for _, v := range values {
		std += (v - mean) * (v - mean)
	}
	if len(values) > 1 {
		std = math.Sqrt(std / float64(len(values)-1))
	}
	return mean, hi - lo, std
}

/*
XBar charts subgroup means. useS picks X̄-S (σ from the mean subgroup
standard deviation) over X̄-R (σ from the mean range); X̄-R is used only while
every subgroup has 10 readings or fewer. Subgroups of one reading say nothing
about within-group spread and are dropped — the caller reports how many.
*/
func XBar(groups []Subgroup, useS bool) (out []Subgroup, grand float64, dropped int) {
	for _, g := range groups {
		if len(g.Values) < 2 {
			dropped++
			continue
		}
		g.N = len(g.Values)
		g.Mean, g.Range, g.StdDev = describe(g.Values)
		if g.N > 10 {
			useS = true
		}
		out = append(out, g)
	}
	if len(out) == 0 {
		return nil, 0, dropped
	}
	var totalN, sumR, sumS float64
	for _, g := range out {
		grand += g.Mean * float64(g.N)
		totalN += float64(g.N)
		sumR += g.Range
		sumS += g.StdDev
	}
	grand /= totalN
	rBar, sBar := sumR/float64(len(out)), sumS/float64(len(out))
	for i := range out {
		c, _ := Constants(out[i].N)
		if useS {
			sigma := sBar / c.C4
			out[i].Limits = Limits{Center: r4(grand), UCL: r4(grand + c.A3*sBar), LCL: r4(grand - c.A3*sBar), Sigma: r4(sigma / math.Sqrt(float64(out[i].N)))}
			out[i].SpreadLimits = Limits{Center: r4(sBar), UCL: r4(c.B4 * sBar), LCL: r4(c.B3 * sBar)}
		} else {
			sigma := rBar / c.D2
			out[i].Limits = Limits{Center: r4(grand), UCL: r4(grand + c.A2*rBar), LCL: r4(grand - c.A2*rBar), Sigma: r4(sigma / math.Sqrt(float64(out[i].N)))}
			out[i].SpreadLimits = Limits{Center: r4(rBar), UCL: r4(c.D4 * rBar), LCL: r4(c.D3 * rBar)}
		}
		out[i].Mean, out[i].Range, out[i].StdDev = r4(out[i].Mean), r4(out[i].Range), r4(out[i].StdDev)
	}
	return out, r4(grand), dropped
}

// Proportion is one subgroup on a p chart: Defective of Inspected failed.
type Proportion struct {
	Key       string  `json:"key"`
	Inspected int     `json:"inspected"`
	Defective int     `json:"defective"`
	P         float64 `json:"p"`
	Limits    Limits  `json:"limits"`
}

// PChart sets the limits for fractions failing, per subgroup size.
func PChart(groups []Proportion) []Proportion {
	var d, n int
	for _, g := range groups {
		d += g.Defective
		n += g.Inspected
	}
	out := []Proportion{}
	if n == 0 {
		return out
	}
	pBar := float64(d) / float64(n)
	for _, g := range groups {
		if g.Inspected == 0 {
			continue
		}
		g.P = r4(float64(g.Defective) / float64(g.Inspected))
		sigma := math.Sqrt(pBar * (1 - pBar) / float64(g.Inspected))
		g.Limits = Limits{Center: r4(pBar), UCL: r4(math.Min(1, pBar+3*sigma)), LCL: r4(math.Max(0, pBar-3*sigma)), Sigma: r4(sigma)}
		out = append(out, g)
	}
	return out
}

// CChart sets the limits for counts of defects per unit.
func CChart(counts []float64) Limits {
	if len(counts) == 0 {
		return Limits{}
	}
	c := 0.0
	for _, v := range counts {
		c += v
	}
	c /= float64(len(counts))
	s := math.Sqrt(c)
	return Limits{Center: r4(c), UCL: r4(c + 3*s), LCL: r4(math.Max(0, c-3*s)), Sigma: r4(s)}
}

/*
Rule names the Nelson tests this service applies. A process in control breaks
each by chance only rarely; together they catch a shift (2), a drift (3) and a
change in spread (5, 6) well before a point crosses a limit (1).
*/
var Rules = map[int]string{
	1: "one point beyond 3σ",
	2: "nine points in a row on one side of the centre",
	3: "six points in a row steadily increasing or decreasing",
	5: "two of three points beyond 2σ on the same side",
	6: "four of five points beyond 1σ on the same side",
}

// DefaultRules is what a parameter is tested against unless configured.
var DefaultRules = []int{1, 2, 3, 5, 6}

/*
ApplyRules returns, for each point, the rules it completes. limits may be one
band for every point or one per point (subgroups of different sizes). A rule
is reported at the point where its pattern completes, so an alarm fires on the
reading that tipped it — not on every point of the run.
*/
func ApplyRules(values []float64, limits []Limits, enabled []int) [][]int {
	out := make([][]int, len(values))
	if len(values) == 0 || len(limits) == 0 {
		return out
	}
	on := map[int]bool{}
	for _, r := range enabled {
		on[r] = true
	}
	lim := func(i int) Limits {
		if len(limits) == 1 {
			return limits[0]
		}
		return limits[i]
	}
	z := func(i int) float64 { // signed distance from centre in σ
		l := lim(i)
		if l.Sigma <= 0 {
			return 0
		}
		return (values[i] - l.Center) / l.Sigma
	}
	side := func(i int) int {
		d := values[i] - lim(i).Center
		switch {
		case d > 0:
			return 1
		case d < 0:
			return -1
		}
		return 0
	}
	countBeyond := func(end, window int, k float64, s int) int {
		n := 0
		for j := end - window + 1; j <= end; j++ {
			if j >= 0 && float64(s)*z(j) > k {
				n++
			}
		}
		return n
	}
	for i := range values {
		l := lim(i)
		if on[1] && (values[i] > l.UCL || values[i] < l.LCL) {
			out[i] = append(out[i], 1)
		}
		if on[2] && i >= 8 {
			s := side(i)
			run := s != 0
			for j := i - 8; j < i && run; j++ {
				run = side(j) == s
			}
			if run {
				out[i] = append(out[i], 2)
			}
		}
		if on[3] && i >= 5 {
			up, down := true, true
			for j := i - 4; j <= i; j++ {
				up = up && values[j] > values[j-1]
				down = down && values[j] < values[j-1]
			}
			if up || down {
				out[i] = append(out[i], 3)
			}
		}
		if s := side(i); s != 0 && lim(i).Sigma > 0 {
			if on[5] && i >= 2 && float64(s)*z(i) > 2 && countBeyond(i, 3, 2, s) >= 2 {
				out[i] = append(out[i], 5)
			}
			if on[6] && i >= 4 && float64(s)*z(i) > 1 && countBeyond(i, 5, 1, s) >= 4 {
				out[i] = append(out[i], 6)
			}
		}
	}
	return out
}
