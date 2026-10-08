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
