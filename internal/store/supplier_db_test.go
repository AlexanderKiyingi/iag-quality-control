package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"iag-quality-control/backend/internal/db"
	"iag-quality-control/backend/internal/esign"
	"iag-quality-control/backend/internal/migrate"
)

// Supplier quality (023) against a real Postgres. Env-gated; see
// TestUpsertGuardsAgainstRealPostgres.
func TestSupplierQualityAgainstRealPostgres(t *testing.T) {
	dsn := os.Getenv("QC_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("QC_TEST_DATABASE_URL not set — see TestUpsertGuardsAgainstRealPostgres")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool, err := db.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()
	if err := migrate.Up(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	s := New(pool)
	run := time.Now().UTC().Format("150405.000")
	supplier := "vendor-" + run
	str := func(v string) *string { return &v }
	lot := func(i int, result string) IncomingInspection {
		t.Helper()
		item, err := s.UpsertIncomingInspection(ctx, UpsertIncomingInspectionInput{
			SourceLot: fmt.Sprintf("LOT-%d-%s", i, run), Result: str(result), SupplierRef: str(supplier),
			PORef: str("PO-2026-abc"), ReceiptRef: str("GRN-abc"), Actor: "ana@iag"})
		if err != nil {
			t.Fatal(err)
		}
		return item
	}
	card := func() SupplierScorecard {
		t.Helper()
		cards, err := s.SupplierScorecards(ctx, supplier, 365)
		if err != nil || len(cards) != 1 {
			t.Fatalf("scorecard: %+v %v", cards, err)
		}
		return cards[0]
	}

	t.Run("too few lots is not a rating", func(t *testing.T) {
		first := lot(0, "accept")
		if first.SupplierRef != supplier || first.ReceiptRef != "GRN-abc" {
			t.Fatalf("refs: %+v", first)
		}
		if c := card(); c.Recommendation != "insufficient_data" || c.Lots != 1 {
			t.Fatalf("one lot: %+v", c)
		}
	})

	t.Run("accepted lots rate approved; a rejection drops it", func(t *testing.T) {
		for i := 1; i < 20; i++ {
			lot(i, "accept")
		}
		c := card()
		if c.Recommendation != "approved" || c.LotsAccepted != 20 || c.AcceptanceRate == nil || *c.AcceptanceRate != 100 {
			t.Fatalf("20 good lots: %+v", c)
		}
		if changed, err := s.RecordSupplierRecommendation(ctx, c); err != nil || !changed {
			t.Fatalf("first recommendation is news: %v %v", changed, err)
		}
		if changed, _ := s.RecordSupplierRecommendation(ctx, c); changed {
			t.Fatal("the same recommendation is not announced twice")
		}
		lot(20, "reject")
		lot(21, "quarantine")
		c = card()
		if c.Recommendation != "conditional" || c.LotsRejected != 2 || len(c.Trend) == 0 {
			t.Fatalf("2 of 22 rejected: %+v", c)
		}
		if changed, _ := s.RecordSupplierRecommendation(ctx, c); !changed {
			t.Fatal("a changed recommendation is news")
		}
	})

	var scar SCAR
	t.Run("a SCAR from an NC on an inspected lot takes its supplier", func(t *testing.T) {
		bad := lot(30, "reject")
		nc, err := s.UpsertNonConformance(ctx, UpsertNonConformanceInput{BusinessID: "NCR-SUP-" + run, Title: "Wet lot",
			SourceRef: str(bad.BusinessID), Actor: "ana@iag"})
		if err != nil {
			t.Fatal(err)
		}
		scar, err = s.CreateSCAR(ctx, SCARInput{NCRef: &nc.BusinessID, Title: str("Moisture over 12.5% on arrival"),
			Severity: str("critical"), Actor: "ana@iag"})
		if err != nil || scar.SupplierRef != supplier || scar.InspectionRef != bad.BusinessID || scar.Status != "open" || scar.DueDate == nil {
			t.Fatalf("scar: %+v %v", scar, err)
		}
		if c := card(); c.CriticalSCARs != 1 || c.OpenSCARs != 1 || c.NonConformances < 1 {
			t.Fatalf("scorecard counts the SCAR and NC: %+v", c)
		}
		// Named with its supplier too (as from the scorecard), the lot still comes with it.
		named, err := s.CreateSCAR(ctx, SCARInput{NCRef: &nc.BusinessID, SupplierRef: str(supplier), Title: str("again"), Actor: "ana@iag"})
		if err != nil || named.InspectionRef != bad.BusinessID {
			t.Fatalf("named supplier: %+v %v", named, err)
		}
		if _, err := s.CreateSCAR(ctx, SCARInput{NCRef: &nc.BusinessID, SupplierRef: str("someone-else"), Title: str("x")}); !errors.Is(err, ErrBadInput) {
			t.Fatalf("wrong supplier for the NC: %v", err)
		}
		if _, err := s.CreateSCAR(ctx, SCARInput{Title: str("no supplier")}); !errors.Is(err, ErrBadInput) {
			t.Fatalf("no supplier: %v", err)
		}
	})

	t.Run("the supplier answers, the fix is checked, and only a signature closes it", func(t *testing.T) {
		if _, err := s.UpdateSCAR(ctx, scar.BusinessID, SCARInput{SupplierRootCause: str("drying beds overloaded")}); !errors.Is(err, ErrReasonRequired) {
			t.Fatalf("edit without reason: %v", err)
		}
		got, err := s.UpdateSCAR(ctx, scar.BusinessID, SCARInput{SupplierRootCause: str("drying beds overloaded"),
			SupplierAction: str("added two beds"), ChangeReason: "supplier replied", Actor: "ana@iag"})
		if err != nil || got.Status != "response_received" || got.ResponseReceivedAt == nil {
			t.Fatalf("response: %+v %v", got, err)
		}
		if _, err := s.UpdateSCAR(ctx, scar.BusinessID, SCARInput{Status: str("closed"), ChangeReason: "x"}); !errors.Is(err, ErrSignatureState) {
			t.Fatalf("closing by edit: %v", err)
		}
		signer := esign.Signer{Subject: "u-1", Email: "ana@iag", Name: "Ana", AuthTime: time.Now(), JTI: "scar1-" + run}
		if _, err := s.SignRecord(ctx, SignInput{Entity: "scars", BusinessID: scar.BusinessID, Meaning: "closed",
			Reason: "fixed", Signer: signer}); !errors.Is(err, ErrSignatureState) {
			t.Fatalf("closing before the fix is shown effective: %v", err)
		}
		if _, err := s.UpdateSCAR(ctx, scar.BusinessID, SCARInput{Status: str("verifying"), EffectivenessCheck: str("next 3 lots under 12%"),
			Effective: func(b bool) *bool { return &b }(true), ChangeReason: "verified", Actor: "ana@iag"}); err != nil {
			t.Fatal(err)
		}
		signer.JTI = "scar2-" + run
		if _, err := s.SignRecord(ctx, SignInput{Entity: "scars", BusinessID: scar.BusinessID, Meaning: "closed",
			Reason: "three clean lots", Signer: signer}); err != nil {
			t.Fatal(err)
		}
		closed, _ := s.GetSCAR(ctx, scar.BusinessID)
		if closed.Status != "closed" || closed.ClosedAt == nil {
			t.Fatalf("closed: %+v", closed)
		}
		if _, err := s.UpdateSCAR(ctx, scar.BusinessID, SCARInput{Title: str("x"), ChangeReason: "late edit"}); !errors.Is(err, ErrLocked) {
			t.Fatalf("a closed SCAR is locked: %v", err)
		}
		if c := card(); c.OpenSCARs != 1 {
			t.Fatalf("closed SCAR still open on the scorecard: %+v", c)
		}
	})
}
