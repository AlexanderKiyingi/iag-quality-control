package domain

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

/*
Green coffee grading after the SCA Green Arabica Defect Handbook.

A grader counts beans of each defect type in a 350 g sample. Each type has an
equivalent: how many beans make one "full defect" (one full black bean is a
full defect; it takes five floaters). Full defects per type are whole numbers —
four floaters are no full defect — and Category 1 (primary) types are counted
apart from Category 2 (secondary), because a specialty grade allows none of
the first.
*/

// DefectType is one row of the handbook's table.
type DefectType struct {
	Key        string `json:"key"`
	Label      string `json:"label"`
	Category   int    `json:"category"`
	Equivalent int    `json:"equivalent"`
}

// DefectTypes is the SCA Arabica table, in the order a grading sheet lists it.
var DefectTypes = []DefectType{
	{"full_black", "Full black", 1, 1},
	{"full_sour", "Full sour", 1, 1},
	{"dried_cherry", "Dried cherry / pod", 1, 1},
	{"fungus_damaged", "Fungus damaged", 1, 1},
	{"foreign_matter", "Foreign matter", 1, 1},
	{"severe_insect_damage", "Severe insect damage", 1, 5},
	{"partial_black", "Partial black", 2, 3},
	{"partial_sour", "Partial sour", 2, 3},
	{"parchment", "Parchment / pergamino", 2, 5},
	{"floater", "Floater", 2, 5},
	{"immature", "Immature / unripe", 2, 5},
	{"withered", "Withered", 2, 5},
	{"shell", "Shell", 2, 5},
	{"broken", "Broken / chipped / cut", 2, 5},
	{"hull_husk", "Hull / husk", 2, 5},
	{"slight_insect_damage", "Slight insect damage", 2, 10},
}

// StandardSampleGrams is the SCA green grading sample.
const StandardSampleGrams = 350.0

// Screens a grading may weigh, largest first. "pan" is what fell through.
var Screens = []string{"20", "19", "18", "17", "16", "15", "14", "13", "12", "pan"}

// SCA grade classes, best first.
const (
	GreenSpecialty     = "specialty"
	GreenPremium       = "premium"
	GreenExchange      = "exchange"
	GreenBelowStandard = "below_standard"
	GreenOffGrade      = "off_grade"
)

// GreenInput is what a grader records.
type GreenInput struct {
	SampleGrams   float64
	Defects       map[string]int     // defect key → bean count
	Screens       map[string]float64 // screen → grams retained
	MoisturePct   *float64
	WaterActivity *float64
	DensityGL     *float64
	// Quakers in the 100 g roast of the same lot; nil when not roasted.
	Quakers *int
}

// GreenResult is what the grading works out.
type GreenResult struct {
	FullDefects   map[string]int     `json:"full_defects_by_type"`
	Category1     int                `json:"category1_full_defects"`
	Category2     int                `json:"category2_full_defects"`
	Total         int                `json:"full_defects"`
	TotalPer350   float64            `json:"full_defects_per_350g"`
	ScreenPct     map[string]float64 `json:"screen_pct"`
	ScreenPlusPct map[string]float64 `json:"screen_plus_pct"`
	Grade         string             `json:"sca_grade"`
	GradeReasons  []string           `json:"grade_reasons"`
	// Metrics is what the spec engine and SPC read, by canonical parameter.
	Metrics map[string]float64 `json:"metrics"`
}

func defectType(key string) (DefectType, bool) {
	for _, d := range DefectTypes {
		if d.Key == key {
			return d, true
		}
	}
	return DefectType{}, false
}

// ValidateGreen refuses input a grade cannot honestly be computed from.
func ValidateGreen(in GreenInput) error {
	if in.SampleGrams < 0 || in.SampleGrams > 5000 {
		return fmt.Errorf("sample weight must be between 0 and 5000 g")
	}
	for k, n := range in.Defects {
		if _, ok := defectType(k); !ok {
			return fmt.Errorf("%q is not a defect type", k)
		}
		if n < 0 {
			return fmt.Errorf("the %s count cannot be negative", k)
		}
	}
	known := map[string]bool{}
	for _, s := range Screens {
		known[s] = true
	}
	for k, g := range in.Screens {
		if !known[k] {
			return fmt.Errorf("screen %q is not one of %s", k, strings.Join(Screens, ", "))
		}
		if g < 0 || math.IsNaN(g) {
			return fmt.Errorf("screen %s weight cannot be negative", k)
		}
	}
	if in.MoisturePct != nil && (*in.MoisturePct < 0 || *in.MoisturePct > 100) {
		return fmt.Errorf("moisture must be a percentage")
	}
	if in.WaterActivity != nil && (*in.WaterActivity < 0 || *in.WaterActivity > 1) {
		return fmt.Errorf("water activity is between 0 and 1")
	}
	if in.Quakers != nil && *in.Quakers < 0 {
		return fmt.Errorf("quakers cannot be negative")
	}
	return nil
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }

// GradeGreen works out full defects, the screen distribution and the SCA grade.
func GradeGreen(in GreenInput) GreenResult {
	grams := in.SampleGrams
	if grams <= 0 {
		grams = StandardSampleGrams
	}
	r := GreenResult{FullDefects: map[string]int{}, ScreenPct: map[string]float64{},
		ScreenPlusPct: map[string]float64{}, Metrics: map[string]float64{}, GradeReasons: []string{}}
	for _, d := range DefectTypes {
		n := in.Defects[d.Key]
		if n == 0 {
			continue
		}
		full := n / d.Equivalent
		r.FullDefects[d.Key] = full
		if d.Category == 1 {
			r.Category1 += full
		} else {
			r.Category2 += full
		}
	}
	r.Total = r.Category1 + r.Category2
	r.TotalPer350 = round1(float64(r.Total) * StandardSampleGrams / grams)

	var screened float64
	for _, g := range in.Screens {
		screened += g
	}
	if screened > 0 {
		var cumulative float64
		for _, s := range Screens {
			g, ok := in.Screens[s]
			if ok {
				r.ScreenPct[s] = round1(g / screened * 100)
			}
			cumulative += g
			if s != "pan" && ok {
				r.ScreenPlusPct[s] = round1(cumulative / screened * 100)
			}
		}
	}

	r.Grade, r.GradeReasons = classify(r, in)

	r.Metrics["full_defects"] = r.TotalPer350
	r.Metrics["category1_full_defects"] = float64(r.Category1)
	r.Metrics["category2_full_defects"] = float64(r.Category2)
	r.Metrics["defects"] = float64(r.Total)
	for s, pct := range r.ScreenPlusPct {
		r.Metrics["screen_"+s+"_plus_pct"] = pct
	}
	if in.MoisturePct != nil {
		r.Metrics["moisture"] = *in.MoisturePct
	}
	if in.WaterActivity != nil {
		r.Metrics["water_activity"] = *in.WaterActivity
	}
	if in.DensityGL != nil {
		r.Metrics["density_g_l"] = *in.DensityGL
	}
	if in.Quakers != nil {
		r.Metrics["quakers"] = float64(*in.Quakers)
	}
	return r
}

/*
classify applies the SCA green grade thresholds to a 350 g sample:

	specialty       no Category 1, ≤ 5 full defects, no quakers
	premium         ≤ 8 full defects, ≤ 3 quakers
	exchange        9–23 full defects, ≤ 5 quakers
	below standard  24–86 full defects
	off grade       more than 86

The three top grades also need moisture between 9 and 13 %. A grade is never
better than the evidence: unmeasured moisture or unroasted quakers are named
in the reasons rather than assumed good.
*/
func classify(r GreenResult, in GreenInput) (string, []string) {
	reasons := []string{}
	d := r.TotalPer350
	grade := GreenOffGrade
	switch {
	case d <= 5 && r.Category1 == 0:
		grade = GreenSpecialty
	case d <= 8:
		grade = GreenPremium
		if r.Category1 > 0 && d <= 5 {
			reasons = append(reasons, fmt.Sprintf("%d Category 1 full defect(s) rule out specialty", r.Category1))
		}
	case d <= 23:
		grade = GreenExchange
	case d <= 86:
		grade = GreenBelowStandard
	}
	if d > 5 {
		reasons = append(reasons, fmt.Sprintf("%s full defects per 350 g", trimFloat(d)))
	}
	if in.Quakers != nil {
		q := *in.Quakers
		for _, step := range []struct {
			grade string
			max   int
		}{{GreenSpecialty, 0}, {GreenPremium, 3}, {GreenExchange, 5}} {
			if grade == step.grade && q > step.max {
				grade = nextGrade(grade)
				reasons = append(reasons, fmt.Sprintf("%d quaker(s) in the roast", q))
			}
		}
	} else if grade == GreenSpecialty || grade == GreenPremium || grade == GreenExchange {
		reasons = append(reasons, "quakers not assessed")
	}
	if in.MoisturePct != nil {
		m := *in.MoisturePct
		if (m < 9 || m > 13) && rank(grade) < rank(GreenBelowStandard) {
			grade = GreenBelowStandard
			reasons = append(reasons, fmt.Sprintf("moisture %s%% is outside 9–13%%", trimFloat(m)))
		}
	} else if rank(grade) < rank(GreenBelowStandard) {
		reasons = append(reasons, "moisture not measured")
	}
	return grade, reasons
}

var gradeOrder = []string{GreenSpecialty, GreenPremium, GreenExchange, GreenBelowStandard, GreenOffGrade}

func rank(g string) int {
	for i, v := range gradeOrder {
		if v == g {
			return i
		}
	}
	return len(gradeOrder)
}

func nextGrade(g string) string {
	if i := rank(g); i+1 < len(gradeOrder) {
		return gradeOrder[i+1]
	}
	return GreenOffGrade
}

func trimFloat(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// SortedMetricKeys lists a result's metrics in a stable order.
func SortedMetricKeys(m map[string]float64) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
