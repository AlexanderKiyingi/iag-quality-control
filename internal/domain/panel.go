package domain

import (
	"math"
	"sort"
)

// CuppingAttributes is the SCA sheet's ten scored attributes, in sheet order.
var CuppingAttributes = []string{
	"fragrance", "flavor", "aftertaste", "acidity", "body",
	"balance", "uniformity", "cleancup", "sweetness", "overall",
}

// DefaultOutlierPoints is how far an evaluator's total may sit from the panel
// mean before they are flagged. Two points is about where calibrated Q-graders
// stop agreeing on the same cup; a lab that calibrates tighter can pass its own.
const DefaultOutlierPoints = 2.0

// PanelScore is one evaluator's sheet.
type PanelScore struct {
	Evaluator  string
	Scores     map[string]float64
	DefectCat1 int
	DefectCat2 int
}

// Total is the evaluator's SCA total: attribute sum less defect penalties.
func (p PanelScore) Total() float64 {
	return CalcSCATotal(p.Scores, p.DefectCat1, p.DefectCat2)
}

// Spread summarises one quantity across the panel.
type Spread struct {
	Mean   float64 `json:"mean"`
	StdDev float64 `json:"std_dev"`
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
}

// EvaluatorDeviation is how far one evaluator sat from the rest.
type EvaluatorDeviation struct {
	Evaluator string  `json:"evaluator"`
	Total     float64 `json:"total"`
	Deviation float64 `json:"deviation"`
	Outlier   bool    `json:"outlier"`
}

// PanelStats is the variance picture of a cupping panel.
type PanelStats struct {
	PanelSize        int                  `json:"panel_size"`
	OutlierThreshold float64              `json:"outlier_threshold"`
	Attributes       map[string]Spread    `json:"attributes"`
	Total            Spread               `json:"total"`
	Evaluators       []EvaluatorDeviation `json:"evaluators"`
	OutlierCount     int                  `json:"outlier_count"`
}

/*
ComputePanelStats measures agreement across a panel.

Standard deviation is the sample deviation (n-1): a panel is a sample of the
cuppers who could have scored the coffee, not the population.

An evaluator is an outlier when their total is more than threshold points from
the panel MEDIAN. Not the mean: one wild score drags a mean toward itself, which
both hides the outlier in a small panel and pushes the cuppers who agree with
each other away from it — with four cuppers at 82.5 / 83 / 82 / 76, a mean-based
check flags three of them. The median of those is 82.25, and only the 76 is
far from it.

Two cuppers have no majority, so neither can be singled out: each deviation is
measured against the other, and both are flagged when they disagree by more than
the threshold. One cupper has no spread at all.
*/
func ComputePanelStats(panel []PanelScore, threshold float64) PanelStats {
	if threshold <= 0 {
		threshold = DefaultOutlierPoints
	}
	stats := PanelStats{
		PanelSize:        len(panel),
		OutlierThreshold: threshold,
		Attributes:       map[string]Spread{},
		Evaluators:       []EvaluatorDeviation{},
	}
	if len(panel) == 0 {
		return stats
	}
	for _, attr := range CuppingAttributes {
		vals := make([]float64, len(panel))
		for i, p := range panel {
			vals[i] = p.Scores[attr]
		}
		stats.Attributes[attr] = spreadOf(vals)
	}
	totals := make([]float64, len(panel))
	for i, p := range panel {
		totals[i] = p.Total()
	}
	stats.Total = spreadOf(totals)
	median := medianOf(totals)
	for i, p := range panel {
		dev := 0.0
		switch {
		case len(panel) == 2:
			dev = round2(totals[i] - totals[1-i])
		case len(panel) > 2:
			dev = round2(totals[i] - median)
		}
		outlier := math.Abs(dev) > threshold
		if outlier {
			stats.OutlierCount++
		}
		stats.Evaluators = append(stats.Evaluators, EvaluatorDeviation{
			Evaluator: p.Evaluator, Total: round2(totals[i]), Deviation: dev, Outlier: outlier,
		})
	}
	sort.SliceStable(stats.Evaluators, func(a, b int) bool {
		return math.Abs(stats.Evaluators[a].Deviation) > math.Abs(stats.Evaluators[b].Deviation)
	})
	return stats
}

func spreadOf(vals []float64) Spread {
	if len(vals) == 0 {
		return Spread{}
	}
	mean, lo, hi := 0.0, vals[0], vals[0]
	for _, v := range vals {
		mean += v
		lo = math.Min(lo, v)
		hi = math.Max(hi, v)
	}
	mean /= float64(len(vals))
	std := 0.0
	if len(vals) > 1 {
		for _, v := range vals {
			std += (v - mean) * (v - mean)
		}
		std = math.Sqrt(std / float64(len(vals)-1))
	}
	return Spread{Mean: round2(mean), StdDev: round2(std), Min: lo, Max: hi}
}

func medianOf(vals []float64) float64 {
	sorted := append([]float64(nil), vals...)
	sort.Float64s(sorted)
	n := len(sorted)
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }
