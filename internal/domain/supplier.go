package domain

import "fmt"

/*
Supplier status recommendations.

Procurement decides a supplier's status; quality recommends one from the
evidence, and says why, so the decision can be argued with. The thresholds are
the common incoming-quality bands:

	approved     ≥ 95 % of decided lots accepted, no overdue SCAR, no open critical SCAR
	conditional  ≥ 85 % accepted, or approved-level with an overdue / critical SCAR
	suspended    below 85 %

Fewer than MinLotsForRating decided lots is too little to rate: one rejected
lot of two is not a 50 % supplier. Such a supplier is "insufficient_data"
unless a critical SCAR is open, which on its own makes it conditional.
*/

const (
	SupplierApproved         = "approved"
	SupplierConditional      = "conditional"
	SupplierSuspended        = "suspended"
	SupplierInsufficientData = "insufficient_data"
	MinLotsForRating         = 3
)

// SupplierEvidence is what a recommendation is made from.
type SupplierEvidence struct {
	LotsDecided   int // inspections accepted or rejected
	LotsAccepted  int
	OverdueSCARs  int
	CriticalSCARs int // open, severity critical
}

// RecommendSupplierStatus returns the status quality recommends and why.
func RecommendSupplierStatus(e SupplierEvidence) (string, []string, *float64) {
	reasons := []string{}
	if e.LotsDecided < MinLotsForRating {
		reasons = append(reasons, fmt.Sprintf("%d decided lot(s); %d are needed to rate a supplier", e.LotsDecided, MinLotsForRating))
		if e.CriticalSCARs > 0 {
			return SupplierConditional, append(reasons, fmt.Sprintf("%d open critical SCAR(s)", e.CriticalSCARs)), nil
		}
		return SupplierInsufficientData, reasons, nil
	}
	rate := float64(e.LotsAccepted) / float64(e.LotsDecided) * 100
	rate = float64(int(rate*10+0.5)) / 10
	reasons = append(reasons, fmt.Sprintf("%.1f%% of %d lots accepted", rate, e.LotsDecided))
	status := SupplierSuspended
	switch {
	case rate >= 95:
		status = SupplierApproved
	case rate >= 85:
		status = SupplierConditional
	}
	if status == SupplierApproved && (e.OverdueSCARs > 0 || e.CriticalSCARs > 0) {
		status = SupplierConditional
	}
	if e.OverdueSCARs > 0 {
		reasons = append(reasons, fmt.Sprintf("%d SCAR(s) past due", e.OverdueSCARs))
	}
	if e.CriticalSCARs > 0 {
		reasons = append(reasons, fmt.Sprintf("%d open critical SCAR(s)", e.CriticalSCARs))
	}
	return status, reasons, &rate
}
