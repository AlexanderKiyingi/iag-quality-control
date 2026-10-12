package domain

import "testing"

func cvaAll(v int) CVASheet {
	h := map[string]int{}
	for _, s := range CVAAffectiveSections {
		h[s] = v
	}
	return CVASheet{Hedonic: h}
}

func TestCVAScoreMatchesTheStandardsReferencePoints(t *testing.T) {
	if got := CVAScore(cvaAll(9)); got != 100 {
		t.Fatalf("all 9s must score 100, got %v", got)
	}
	if got := CVAScore(cvaAll(5)); got != 79 {
		t.Fatalf("all 5s must score 79, got %v", got)
	}
	s := cvaAll(7)
	s.NonUniformCups, s.DefectiveCups = 1, 1
	// 0.65625 × 56 + 52.75 = 89.5; − 2 − 4 = 83.5
	if got := CVAScore(s); got != 83.5 {
		t.Fatalf("deductions: got %v", got)
	}
}

func TestValidateCVA(t *testing.T) {
	if err := ValidateCVA(cvaAll(6), 5); err != nil {
		t.Fatal(err)
	}
	missing := cvaAll(6)
	delete(missing.Hedonic, "mouthfeel")
	if ValidateCVA(missing, 5) == nil {
		t.Fatal("a missing section must be refused")
	}
	if ValidateCVA(cvaAll(10), 5) == nil {
		t.Fatal("hedonic is 1–9")
	}
	tooMany := cvaAll(6)
	tooMany.NonUniformCups, tooMany.DefectiveCups = 3, 3
	if ValidateCVA(tooMany, 5) == nil {
		t.Fatal("more flagged cups than cupped")
	}
	loud := cvaAll(6)
	loud.Intensity = map[string]int{"acidity": 16}
	if ValidateCVA(loud, 5) == nil {
		t.Fatal("intensity is 0–15")
	}
}
