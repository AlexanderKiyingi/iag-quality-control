package domain

import "testing"

func gf(v float64) *float64 { return &v }
func gn(v int) *int         { return &v }

func TestGradeGreenFullDefects(t *testing.T) {
	r := GradeGreen(GreenInput{SampleGrams: 350, Defects: map[string]int{
		"full_black": 1, "floater": 4, "broken": 11, "slight_insect_damage": 10, "partial_sour": 3,
	}, MoisturePct: gf(11), Quakers: gn(0)})
	// full black 1; floaters 4/5 = 0; broken 11/5 = 2; slight insect 10/10 = 1; partial sour 3/3 = 1.
	if r.Category1 != 1 || r.Category2 != 4 || r.Total != 5 {
		t.Fatalf("full defects: %+v", r)
	}
	if _, ok := r.FullDefects["floater"]; !ok || r.FullDefects["floater"] != 0 {
		t.Fatalf("four floaters are counted and make no full defect: %+v", r.FullDefects)
	}
	if r.Grade != GreenPremium {
		t.Fatalf("a Category 1 defect rules out specialty even at 5 full defects: %s %v", r.Grade, r.GradeReasons)
	}
}

func TestGradeGreenClasses(t *testing.T) {
	cases := []struct {
		name  string
		in    GreenInput
		grade string
	}{
		{"clean specialty", GreenInput{Defects: map[string]int{"broken": 25}, MoisturePct: gf(10.5), Quakers: gn(0)}, GreenSpecialty},
		{"a quaker drops specialty to premium", GreenInput{Defects: map[string]int{"broken": 25}, MoisturePct: gf(10.5), Quakers: gn(1)}, GreenPremium},
		{"exchange", GreenInput{Defects: map[string]int{"floater": 100}, MoisturePct: gf(11), Quakers: gn(2)}, GreenExchange},
		{"below standard", GreenInput{Defects: map[string]int{"full_black": 30}, MoisturePct: gf(11)}, GreenBelowStandard},
		{"off grade", GreenInput{Defects: map[string]int{"full_black": 87}}, GreenOffGrade},
		{"wet coffee is below standard", GreenInput{MoisturePct: gf(14.2), Quakers: gn(0)}, GreenBelowStandard},
		{"half sample normalised to 350 g", GreenInput{SampleGrams: 175, Defects: map[string]int{"broken": 15}, MoisturePct: gf(11), Quakers: gn(0)}, GreenPremium},
	}
	for _, c := range cases {
		if got := GradeGreen(c.in).Grade; got != c.grade {
			t.Errorf("%s: got %s want %s (%v)", c.name, got, c.grade, GradeGreen(c.in).GradeReasons)
		}
	}
}

func TestGradeGreenNamesWhatWasNotMeasured(t *testing.T) {
	r := GradeGreen(GreenInput{})
	if r.Grade != GreenSpecialty || len(r.GradeReasons) != 2 {
		t.Fatalf("no defects but nothing else measured: %s %v", r.Grade, r.GradeReasons)
	}
}

func TestGradeGreenScreens(t *testing.T) {
	r := GradeGreen(GreenInput{Screens: map[string]float64{"18": 120, "17": 100, "16": 80, "15": 30, "pan": 20}})
	if r.ScreenPct["18"] != 34.3 || r.ScreenPlusPct["18"] != 34.3 || r.ScreenPlusPct["15"] != 94.3 {
		t.Fatalf("screens: %+v %+v", r.ScreenPct, r.ScreenPlusPct)
	}
	if r.Metrics["screen_15_plus_pct"] != 94.3 {
		t.Fatalf("metrics: %+v", r.Metrics)
	}
	if _, ok := r.ScreenPlusPct["pan"]; ok {
		t.Fatal("there is nothing above the pan")
	}
}

func TestValidateGreen(t *testing.T) {
	bad := []GreenInput{
		{Defects: map[string]int{"mouldy": 1}},
		{Defects: map[string]int{"floater": -1}},
		{Screens: map[string]float64{"21": 10}},
		{WaterActivity: gf(1.4)},
	}
	for i, in := range bad {
		if ValidateGreen(in) == nil {
			t.Errorf("case %d should be refused", i)
		}
	}
	if err := ValidateGreen(GreenInput{SampleGrams: 350, Defects: map[string]int{"floater": 3}}); err != nil {
		t.Fatal(err)
	}
}
