package domain

import "testing"

func sheet(evaluator string, each float64) PanelScore {
	scores := map[string]float64{}
	for _, a := range CuppingAttributes {
		scores[a] = each
	}
	return PanelScore{Evaluator: evaluator, Scores: scores}
}

func TestComputePanelStatsFlagsTheOddCupper(t *testing.T) {
	// Three agree at 82.5 / 83 / 82; the fourth scores 7.6 across the board = 76.
	panel := []PanelScore{sheet("A", 8.25), sheet("B", 8.3), sheet("C", 8.2), sheet("D", 7.6)}
	stats := ComputePanelStats(panel, 0)
	if stats.PanelSize != 4 || stats.OutlierThreshold != DefaultOutlierPoints {
		t.Fatalf("size/threshold: %+v", stats)
	}
	if stats.OutlierCount != 1 || stats.Evaluators[0].Evaluator != "D" || !stats.Evaluators[0].Outlier {
		t.Fatalf("D should be the one outlier, listed first: %+v", stats.Evaluators)
	}
	if got := stats.Evaluators[0].Deviation; got != -6.25 {
		t.Fatalf("D's deviation from the panel median (82.25) should be -6.25, got %v", got)
	}
	if stats.Attributes["acidity"].StdDev == 0 || stats.Total.Min != 76 {
		t.Fatalf("spreads: %+v", stats)
	}
}

func TestComputePanelStatsMedianCatchesASmallPanelOutlier(t *testing.T) {
	// Against the mean (82), C sits only 2.0 below — not over the threshold.
	// Against the median (83) it is 3.0 below, which is the honest answer.
	panel := []PanelScore{sheet("A", 8.3), sheet("B", 8.3), sheet("C", 8.0)}
	stats := ComputePanelStats(panel, 2.0)
	if stats.OutlierCount != 1 || stats.Evaluators[0].Evaluator != "C" {
		t.Fatalf("C should be flagged: %+v", stats.Evaluators)
	}
}

func TestComputePanelStatsSingleCupperHasNoSpread(t *testing.T) {
	stats := ComputePanelStats([]PanelScore{sheet("A", 8)}, 0)
	if stats.OutlierCount != 0 || stats.Total.StdDev != 0 || stats.Evaluators[0].Deviation != 0 {
		t.Fatalf("one cupper: %+v", stats)
	}
}

func TestComputePanelStatsTwoCuppersWhoDisagreeAreBothFlagged(t *testing.T) {
	stats := ComputePanelStats([]PanelScore{sheet("A", 8.3), sheet("B", 8.0)}, 2.0)
	if stats.OutlierCount != 2 {
		t.Fatalf("no majority, so both: %+v", stats.Evaluators)
	}
}

func TestCalibrationMeasuresAgainstTheReferenceNotTheMedian(t *testing.T) {
	// The head judge scores 84; two cuppers drift high together. Against the
	// median they agree with each other; against the reference both are off.
	panel := []PanelScore{sheet("Head", 8.4), sheet("A", 8.8), sheet("B", 8.85)}
	median := ComputePanelStats(panel, 2)
	if median.OutlierCount != 1 || median.Evaluators[0].Evaluator != "Head" {
		t.Fatalf("by median, the head judge is the odd one out: %+v", median.Evaluators)
	}
	cal := ComputeCalibration(panel, "Head", 2)
	if cal.Reference != "Head" || cal.OutlierCount != 2 {
		t.Fatalf("by reference, A and B are off target: %+v", cal.Evaluators)
	}
	for _, ev := range cal.Evaluators {
		if ev.Evaluator == "Head" && (ev.Deviation != 0 || ev.Outlier) {
			t.Fatalf("the reference is the target: %+v", ev)
		}
	}
}

func TestCVAPanelUsesTheSheetsOwnScore(t *testing.T) {
	score := func(v float64) *float64 { return &v }
	panel := []PanelScore{
		{Evaluator: "A", Scores: map[string]float64{"mouthfeel": 7}, TotalScore: score(86.5)},
		{Evaluator: "B", Scores: map[string]float64{"mouthfeel": 7}, TotalScore: score(86)},
		{Evaluator: "C", Scores: map[string]float64{"mouthfeel": 5}, TotalScore: score(79)},
	}
	stats := ComputePanelStats(panel, 2)
	if stats.Total.Max != 86.5 || stats.OutlierCount != 1 || stats.Evaluators[0].Evaluator != "C" {
		t.Fatalf("CVA totals: %+v", stats)
	}
	if _, ok := stats.Attributes["mouthfeel"]; !ok {
		t.Fatalf("a CVA panel spreads over the CVA sections: %+v", stats.Attributes)
	}
}
