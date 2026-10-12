package domain

import (
	"fmt"
	"math"
)

// Cupping forms.
const (
	FormSCA2004 = "sca_2004"
	FormSCACVA  = "sca_cva"
)

// CVAAffectiveSections are the eight sections of the SCA Coffee Value
// Assessment's affective assessment (Standard 104-2024), each scored on a
// 1–9 hedonic scale.
var CVAAffectiveSections = []string{
	"fragrance", "aroma", "flavor", "aftertaste", "acidity", "sweetness", "mouthfeel", "overall",
}

// CVADescriptiveSections carry an intensity (0–15) in the descriptive
// assessment (Standard 103-2024); overall has none.
var CVADescriptiveSections = []string{
	"fragrance", "aroma", "flavor", "aftertaste", "acidity", "sweetness", "mouthfeel",
}

// CVASheet is one cupper's Coffee Value Assessment of one sample.
type CVASheet struct {
	Hedonic        map[string]int `json:"hedonic"`
	Intensity      map[string]int `json:"intensity,omitempty"`
	NonUniformCups int            `json:"non_uniform_cups"`
	DefectiveCups  int            `json:"defective_cups"`
}

// ValidateCVA refuses a sheet the score cannot honestly be computed from:
// every affective section scored 1–9, intensities 0–15, and no more flagged
// cups than were cupped.
func ValidateCVA(s CVASheet, cups int) error {
	for _, section := range CVAAffectiveSections {
		v, ok := s.Hedonic[section]
		if !ok {
			return fmt.Errorf("the %s section has no score", section)
		}
		if v < 1 || v > 9 {
			return fmt.Errorf("%s is scored 1–9, not %d", section, v)
		}
	}
	for section, v := range s.Intensity {
		if v < 0 || v > 15 {
			return fmt.Errorf("%s intensity is 0–15, not %d", section, v)
		}
	}
	if cups <= 0 {
		cups = 5
	}
	if s.NonUniformCups < 0 || s.DefectiveCups < 0 || s.NonUniformCups+s.DefectiveCups > cups {
		return fmt.Errorf("non-uniform and defective cups must be between 0 and the %d cups cupped", cups)
	}
	return nil
}

/*
CVAScore is the affective assessment's score (SCA Standard 104-2024):

	0.65625 × Σ(eight hedonic scores) + 52.75 − 2 × non-uniform cups − 4 × defective cups

The constants are the ones that put a sheet of all 9s at 100 and all 5s at 79,
the standard's reference points. Two decimals, like every other score here.
*/
func CVAScore(s CVASheet) float64 {
	sum := 0
	for _, section := range CVAAffectiveSections {
		sum += s.Hedonic[section]
	}
	score := 0.65625*float64(sum) + 52.75 - 2*float64(s.NonUniformCups) - 4*float64(s.DefectiveCups)
	return math.Round(score*100) / 100
}
