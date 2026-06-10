package domain

// SCATier returns the commercial grade label for an SCA total score.
func SCATier(score float64) string {
	switch {
	case score >= 85:
		return "Specialty"
	case score >= 80:
		return "Premium"
	case score >= 75:
		return "Exchange"
	default:
		return "Reject"
	}
}

// CalcSCATotal sums cupping attribute scores and subtracts defect penalties.
func CalcSCATotal(scores map[string]float64, defectCat1, defectCat2 int) float64 {
	total := 0.0
	for _, v := range scores {
		total += v
	}
	total -= float64(defectCat1*4 + defectCat2*2)
	return total
}

// MoisturePass reports whether moisture is within export spec (≤12.5%).
func MoisturePass(moisture float64) bool {
	return moisture > 0 && moisture <= 12.5
}
