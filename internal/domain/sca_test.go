package domain

import "testing"

func TestSCATier(t *testing.T) {
	cases := map[float64]string{
		86.5:  "Specialty",
		85.0:  "Specialty",
		84.9:  "Premium",
		80.0:  "Premium",
		75.0:  "Exchange",
		74.9:  "Reject",
	}
	for score, want := range cases {
		if got := SCATier(score); got != want {
			t.Fatalf("SCATier(%v) = %q, want %q", score, got, want)
		}
	}
}

func TestCalcSCATotal(t *testing.T) {
	scores := map[string]float64{
		"fragrance": 8, "flavor": 8, "aftertaste": 8, "acidity": 8, "body": 8,
		"balance": 8, "uniformity": 10, "cleancup": 10, "sweetness": 10, "overall": 8,
	}
	got := CalcSCATotal(scores, 0, 3)
	want := 80.0
	if got != want {
		t.Fatalf("CalcSCATotal() = %v, want %v", got, want)
	}
}

func TestMoisturePass(t *testing.T) {
	if !MoisturePass(11.2) {
		t.Fatal("expected 11.2 to pass")
	}
	if MoisturePass(12.6) {
		t.Fatal("expected 12.6 to fail")
	}
}
