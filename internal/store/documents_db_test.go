package store

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"iag-quality-control/backend/internal/db"
	"iag-quality-control/backend/internal/esign"
	"iag-quality-control/backend/internal/migrate"
)

// Controlled documents (025) against a real Postgres. Env-gated; see
// TestUpsertGuardsAgainstRealPostgres.
func TestDocumentControlAgainstRealPostgres(t *testing.T) {
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
	str := func(v string) *string { return &v }
	doc := "SOP-MOIST-" + run
	signer := func(j string) esign.Signer {
		return esign.Signer{Subject: "u-1", Email: "qa@iag", Name: "QA Lead", AuthTime: time.Now(), JTI: j + run}
	}

	d, err := s.CreateDocument(ctx, DocumentInput{BusinessID: doc, Title: str("Moisture by oven"), DocType: str("sop"),
		ReviewPeriodMonths: func(v int) *int { return &v }(12), AttachmentID: "ATT-00001", Filename: "sop-moist-v1.pdf", Actor: "qa@iag"})
	if err != nil || d.Latest == nil || d.Latest.Status != "draft" || d.Effective != nil {
		t.Fatalf("create: %+v %v", d, err)
	}

	// A method run by this SOP cannot be used until the SOP is in force.
	method := "LM-MOIST-" + run
	if _, err := s.UpsertLabMethod(ctx, UpsertLabMethodInput{BusinessID: method, Name: "Oven moisture", SopRef: str(doc)}); err != nil {
		t.Fatal(err)
	}
	sample, err := s.CreateSample(ctx, CreateSampleInput{BatchBusinessID: "BAT-DOC-" + run, SampleID: "SMP-DOC-" + run})
	if err != nil {
		t.Fatal(err)
	}
	measure := func() error {
		_, err := s.CreateLabMeasurement(ctx, CreateLabMeasurementInput{SampleBusinessID: sample.BusinessID,
			Parameter: "moisture", Value: "11", MethodID: method, Actor: "ana@iag"})
		return err
	}
	t.Run("a method's SOP must be in force", func(t *testing.T) {
		if err := measure(); !errors.Is(err, ErrSOPNotEffective) {
			t.Fatalf("draft SOP: %v", err)
		}
	})

	t.Run("draft → review → signed approval → effective", func(t *testing.T) {
		if _, err := s.SignRecord(ctx, SignInput{Entity: "document-versions", BusinessID: doc + "-v1", Meaning: "approved",
			Reason: "ok", Signer: signer("a0")}); !errors.Is(err, ErrSignatureState) {
			t.Fatalf("approving a draft: %v", err)
		}
		if _, err := s.SubmitForReview(ctx, doc, 1, "qa@iag"); err != nil {
			t.Fatal(err)
		}
		// Sent back, edited, resubmitted.
		if _, err := s.SignRecord(ctx, SignInput{Entity: "document-versions", BusinessID: doc + "-v1", Meaning: "rejected",
			Reason: "step 4 unclear", Signer: signer("r1")}); err != nil {
			t.Fatal(err)
		}
		if v, err := s.UpdateDraft(ctx, doc, 1, DocumentInput{AttachmentID: "ATT-00002", Filename: "sop-moist-v1b.pdf", Actor: "qa@iag"}); err != nil || v.Status != "draft" || v.AttachmentID != "ATT-00002" {
			t.Fatalf("edit after rejection: %+v %v", v, err)
		}
		if _, err := s.SubmitForReview(ctx, doc, 1, "qa@iag"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.MakeEffective(ctx, doc, 1, "", "qa@iag"); !errors.Is(err, ErrConflict) {
			t.Fatalf("effective before approval: %v", err)
		}
		if _, err := s.SignRecord(ctx, SignInput{Entity: "document-versions", BusinessID: doc + "-v1", Meaning: "approved",
			Reason: "reviewed", Signer: signer("a1")}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.UpdateDraft(ctx, doc, 1, DocumentInput{ChangeSummary: "x"}); !errors.Is(err, ErrConflict) {
			t.Fatalf("an approved version is not edited: %v", err)
		}
		v, err := s.MakeEffective(ctx, doc, 1, "", "qa@iag")
		if err != nil || v.Status != "effective" || v.NextReviewDate == nil {
			t.Fatalf("effective: %+v %v", v, err)
		}
		if err := measure(); err != nil {
			t.Fatalf("the SOP is in force now: %v", err)
		}
	})

	t.Run("a new version supersedes the old one; one effective at a time", func(t *testing.T) {
		if _, err := s.NewDocumentVersion(ctx, doc, DocumentInput{Actor: "qa@iag"}); !errors.Is(err, ErrBadInput) {
			t.Fatalf("a version says what changes: %v", err)
		}
		v2, err := s.NewDocumentVersion(ctx, doc, DocumentInput{ChangeSummary: "drying time 3h → 4h", AttachmentID: "ATT-00003", Actor: "qa@iag"})
		if err != nil || v2.Version != 2 {
			t.Fatalf("v2: %+v %v", v2, err)
		}
		if _, err := s.NewDocumentVersion(ctx, doc, DocumentInput{ChangeSummary: "again", Actor: "qa@iag"}); !errors.Is(err, ErrConflict) {
			t.Fatalf("one revision at a time: %v", err)
		}
		_, _ = s.SubmitForReview(ctx, doc, 2, "qa@iag")
		if _, err := s.SignRecord(ctx, SignInput{Entity: "document-versions", BusinessID: doc + "-v2", Meaning: "approved",
			Reason: "ok", Signer: signer("a2")}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.MakeEffective(ctx, doc, 2, "", "qa@iag"); err != nil {
			t.Fatal(err)
		}
		d, _ := s.GetDocument(ctx, doc)
		if d.Effective == nil || d.Effective.Version != 2 || d.Versions[1].Status != "superseded" || d.Versions[1].RetiredAt == nil {
			t.Fatalf("supersede: %+v", d.Versions)
		}
	})

	t.Run("read and understood, on the effective version only", func(t *testing.T) {
		if _, err := s.Acknowledge(ctx, doc, 1, "u-2", "ana@iag", "Ana"); !errors.Is(err, ErrConflict) {
			t.Fatalf("a superseded version: %v", err)
		}
		if _, err := s.Acknowledge(ctx, doc, 2, "u-2", "Ana@IAG", "Ana"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Acknowledge(ctx, doc, 2, "u-2", "ana@iag", "Ana"); err != nil {
			t.Fatalf("twice is harmless: %v", err)
		}
		acks, _ := s.ListAcknowledgements(ctx, "", "ana@iag")
		if len(acks) == 0 || acks[0].Version != 2 {
			t.Fatalf("by person: %+v", acks)
		}
		d, _ := s.GetDocument(ctx, doc)
		if d.Effective.Acknowledgements != 1 {
			t.Fatalf("count: %+v", d.Effective)
		}
	})

	t.Run("withdrawing a document needs a reason and stops its method", func(t *testing.T) {
		if _, err := s.ObsoleteDocument(ctx, doc, "", "qa@iag"); !errors.Is(err, ErrReasonRequired) {
			t.Fatalf("no reason: %v", err)
		}
		if _, err := s.ObsoleteDocument(ctx, doc, "replaced by NIR method", "qa@iag"); err != nil {
			t.Fatal(err)
		}
		if err := measure(); !errors.Is(err, ErrSOPNotEffective) {
			t.Fatalf("obsolete SOP: %v", err)
		}
		if _, err := s.NewDocumentVersion(ctx, doc, DocumentInput{ChangeSummary: "x"}); !errors.Is(err, ErrConflict) {
			t.Fatalf("revising an obsolete document: %v", err)
		}
		if _, err := s.CreateDocument(ctx, DocumentInput{BusinessID: doc, Title: str("dup")}); !errors.Is(err, ErrConflict) {
			t.Fatalf("duplicate number: %v", err)
		}
	})
}
