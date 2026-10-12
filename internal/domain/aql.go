package domain

import (
	"fmt"
	"math"
	"strings"
)

/*
AQL sampling (ISO 2859-1 / ANSI Z1.4), single sampling, normal inspection.

Table 1 turns a lot size and an inspection level into a sample-size code
letter; Table 2-A turns the letter and the AQL into a sample size and the
acceptance / rejection numbers. Table 2-A is regular: the cell with Ac 0 sits
where n × AQL ≈ 0.13, and acceptance numbers climb 1, 2, 3, 5, 7, 10, 14, 21
one AQL column at a time; where no plan fits, an arrow sends you to the first
plan above or below, which changes the sample size. That structure is what is
encoded here, and the tests pin the published cells.

Only percent-nonconforming AQLs (0.010 – 10) are supported; tightened and
reduced inspection (Tables 2-B, 2-C) and the switching rules are not.
*/

// InspectionLevels are Table 1's columns.
var InspectionLevels = []string{"S-1", "S-2", "S-3", "S-4", "I", "II", "III"}

var lotBounds = []int{8, 15, 25, 50, 90, 150, 280, 500, 1200, 3200, 10000, 35000, 150000, 500000}

// codeLetters[level][lot band]; bands follow lotBounds, then 500 001 and over.
var codeLetters = map[string]string{
	"S-1": "AAAABBBBCCCCDDD",
	"S-2": "AAABBBCCCDDDEEE",
	"S-3": "AABBCCDDEEFFGGH",
	"S-4": "AABCCDEEFGGHJJK",
	"I":   "AABCCDEFGHJKLMN",
	"II":  "ABCDEFGHJKLMNPQ",
	"III": "BCDEFGHJKLMNPQR",
}

// Letters in Table 2-A order, with their sample sizes.
const letters = "ABCDEFGHJKLMNPQR"

var sampleSizes = []int{2, 3, 5, 8, 13, 20, 32, 50, 80, 125, 200, 315, 500, 800, 1250, 2000}

// AQLs are Table 2-A's percent-nonconforming columns.
var AQLs = []float64{0.010, 0.015, 0.025, 0.040, 0.065, 0.10, 0.15, 0.25, 0.40, 0.65, 1.0, 1.5, 2.5, 4.0, 6.5, 10}

// acceptance numbers along a diagonal, from the third step past Ac 0.
var acceptance = []int{1, 2, 3, 5, 7, 10, 14, 21}

// SamplingPlan is what an inspector does with one lot.
type SamplingPlan struct {
	LotSize        int     `json:"lot_size"`
	Level          string  `json:"level"`
	AQL            float64 `json:"aql"`
	CodeLetter     string  `json:"code_letter"`
	SampleSize     int     `json:"sample_size"`
	Accept         int     `json:"accept"`
	Reject         int     `json:"reject"`
	PlanLetter     string  `json:"plan_letter"` // the letter whose plan was used, when an arrow moved it
	HundredPercent bool    `json:"hundred_percent"`
	Note           string  `json:"note,omitempty"`
}

// CodeLetter is Table 1.
func CodeLetter(lotSize int, level string) (string, error) {
	level = strings.ToUpper(strings.TrimSpace(level))
	row, ok := codeLetters[level]
	if !ok {
		return "", fmt.Errorf("inspection level is one of %s", strings.Join(InspectionLevels, ", "))
	}
	if lotSize < 2 {
		return "", fmt.Errorf("a lot has at least 2 units")
	}
	band := len(lotBounds)
	for i, b := range lotBounds {
		if lotSize <= b {
			band = i
			break
		}
	}
	return string(row[band]), nil
}

func aqlIndex(aql float64) int {
	for i, v := range AQLs {
		if math.Abs(v-aql) < 1e-9 {
			return i
		}
	}
	return -1
}

// Plan is Table 2-A for a code letter and AQL, following the arrows.
func Plan(letter string, aql float64) (sampleSize, accept, reject int, used string, err error) {
	i := strings.Index(letters, strings.ToUpper(strings.TrimSpace(letter)))
	j := aqlIndex(aql)
	if i < 0 {
		return 0, 0, 0, "", fmt.Errorf("code letter %q is not in Table 2-A", letter)
	}
	if j < 0 {
		return 0, 0, 0, "", fmt.Errorf("AQL %g is not a Table 2-A column (0.010 – 10)", aql)
	}
	for steps := 0; steps < 32; steps++ {
		if i < 0 || i >= len(letters) {
			return 0, 0, 0, "", fmt.Errorf("no single sampling plan for letter %s at AQL %g", letter, aql)
		}
		k := j - (14 - i) // 0 is the Ac 0 cell on this row
		switch {
		case k < 0, k == 2:
			i++ // ↓ the first plan below
		case k == 1:
			if i == 0 {
				i++ // nothing above the top row
			} else {
				i-- // ↑ the first plan above
			}
		case k == 0:
			return sampleSizes[i], 0, 1, string(letters[i]), nil
		case k <= 10:
			ac := acceptance[k-3]
			return sampleSizes[i], ac, ac + 1, string(letters[i]), nil
		default:
			i-- // ↑ past Ac 21
		}
	}
	return 0, 0, 0, "", fmt.Errorf("no single sampling plan for letter %s at AQL %g", letter, aql)
}

/*
NewSamplingPlan picks the plan for a lot. When the sample would be the whole
lot or more, every unit is inspected and the acceptance number stands — the
standard's own instruction.
*/
func NewSamplingPlan(lotSize int, level string, aql float64) (SamplingPlan, error) {
	if strings.TrimSpace(level) == "" {
		level = "II"
	}
	letter, err := CodeLetter(lotSize, level)
	if err != nil {
		return SamplingPlan{}, err
	}
	n, ac, re, used, err := Plan(letter, aql)
	if err != nil {
		return SamplingPlan{}, err
	}
	p := SamplingPlan{LotSize: lotSize, Level: strings.ToUpper(strings.TrimSpace(level)), AQL: aql, CodeLetter: letter,
		SampleSize: n, Accept: ac, Reject: re, PlanLetter: used}
	if used != letter {
		p.Note = fmt.Sprintf("Table 2-A points from letter %s to the plan for %s", letter, used)
	}
	if n >= lotSize {
		p.SampleSize, p.HundredPercent = lotSize, true
		p.Note = strings.TrimSpace(p.Note + " — the sample covers the lot, so inspect every unit")
	}
	return p, nil
}

// Judge applies a plan to the nonconforming units found.
func (p SamplingPlan) Judge(nonconforming int) string {
	if nonconforming <= p.Accept {
		return VerdictPass
	}
	return VerdictFail
}
