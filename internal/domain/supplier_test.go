package domain

import "testing"

func TestRecommendSupplierStatus(t *testing.T) {
	cases := []struct {
		name string
		e    SupplierEvidence
		want string
	}{
		{"too few lots", SupplierEvidence{LotsDecided: 2, LotsAccepted: 1}, SupplierInsufficientData},
		{"too few lots but a critical SCAR", SupplierEvidence{LotsDecided: 1, CriticalSCARs: 1}, SupplierConditional},
		{"clean", SupplierEvidence{LotsDecided: 20, LotsAccepted: 20}, SupplierApproved},
		{"exactly 95%", SupplierEvidence{LotsDecided: 20, LotsAccepted: 19}, SupplierApproved},
		{"clean but a SCAR is overdue", SupplierEvidence{LotsDecided: 20, LotsAccepted: 20, OverdueSCARs: 1}, SupplierConditional},
		{"90%", SupplierEvidence{LotsDecided: 10, LotsAccepted: 9}, SupplierConditional},
		{"80%", SupplierEvidence{LotsDecided: 10, LotsAccepted: 8}, SupplierSuspended},
	}
	for _, c := range cases {
		got, reasons, rate := RecommendSupplierStatus(c.e)
		if got != c.want || len(reasons) == 0 {
			t.Errorf("%s: got %s (%v) want %s", c.name, got, reasons, c.want)
		}
		if c.e.LotsDecided >= MinLotsForRating && rate == nil {
			t.Errorf("%s: a rated supplier has a rate", c.name)
		}
	}
}
