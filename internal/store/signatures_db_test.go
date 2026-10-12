package store

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"iag-quality-control/backend/internal/db"
	"iag-quality-control/backend/internal/esign"
	"iag-quality-control/backend/internal/migrate"
)

// Electronic signatures, locking and change reasons (019) against a real
// Postgres. Env-gated; see TestUpsertGuardsAgainstRealPostgres.
func TestSignaturesAgainstRealPostgres(t *testing.T) {
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
	signer := func(jti string) esign.Signer {
		return esign.Signer{Subject: "u-1", Email: "ana@iag", Name: "Ana", AuthTime: time.Now(), JTI: jti + run}
	}

	nc := "NCR-SIG-" + run
	if _, err := s.UpsertNonConformance(ctx, UpsertNonConformanceInput{BusinessID: nc, Title: "Wet lot", Actor: "ana@iag"}); err != nil {
		t.Fatal(err)
	}

	t.Run("a change to an existing record needs a reason, which the change log keeps", func(t *testing.T) {
		_, err := s.UpsertNonConformance(ctx, UpsertNonConformanceInput{BusinessID: nc, Title: "Wet lot", RootCause: str("seal"), Actor: "ana@iag"})
		if !errors.Is(err, ErrReasonRequired) {
			t.Fatalf("want ErrReasonRequired, got %v", err)
		}
		if _, err := s.UpsertNonConformance(ctx, UpsertNonConformanceInput{BusinessID: nc, Title: "Wet lot",
			RootCause: str("bag seal failed"), Actor: "ana@iag", ChangeReason: "root cause found on re-inspection"}); err != nil {
			t.Fatal(err)
		}
		log, err := s.ListChangeLog(ctx, "non-conformances", nc, "", 10)
		if err != nil || len(log) < 2 || log[0].Reason != "root cause found on re-inspection" {
			t.Fatalf("latest entry should carry the reason: %+v %v", log, err)
		}
		if _, leaked := log[0].Changes["change_reason"]; leaked {
			t.Fatal("the reason belongs in its own column, not in the before→after diff")
		}
	})

	var closing Signature
	t.Run("signing closes the NC, fingerprints it, and locks it", func(t *testing.T) {
		var err error
		closing, err = s.SignRecord(ctx, SignInput{Entity: "non-conformances", BusinessID: nc, Meaning: "closed",
			Reason: "CAPA verified effective", Signer: signer("close")})
		if err != nil {
			t.Fatal(err)
		}
		got, _ := s.GetNonConformance(ctx, nc)
		if got.Status != "closed" || len(closing.RecordHash) != 64 || closing.SignerEmail != "ana@iag" {
			t.Fatalf("closed + hashed: %+v / %+v", got, closing)
		}
		if ok, err := s.VerifySignature(ctx, closing); err != nil || !ok {
			t.Fatalf("the record should still match its signature: %v %v", ok, err)
		}
		_, err = s.UpsertNonConformance(ctx, UpsertNonConformanceInput{BusinessID: nc, Title: "Edited after close",
			Actor: "ben@iag", ChangeReason: "tidy"})
		if !errors.Is(err, ErrLocked) {
			t.Fatalf("a signed, closed NC is locked: got %v", err)
		}
		_, err = s.SignRecord(ctx, SignInput{Entity: "non-conformances", BusinessID: nc, Meaning: "closed",
			Reason: "again", Signer: signer("close-again")})
		if !errors.Is(err, ErrLocked) {
			t.Fatalf("closing twice: want ErrLocked, got %v", err)
		}
	})

	t.Run("a change made behind the service's back no longer matches the signature", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `UPDATE qc_non_conformances SET root_cause = 'rewritten' WHERE business_id = $1`, nc); err != nil {
			t.Fatal(err)
		}
		if ok, _ := s.VerifySignature(ctx, closing); ok {
			t.Fatal("a tampered record must not verify")
		}
	})

	t.Run("a signature token signs one thing", func(t *testing.T) {
		other := "NCR-SIG2-" + run
		if _, err := s.UpsertNonConformance(ctx, UpsertNonConformanceInput{BusinessID: other, Title: "Second"}); err != nil {
			t.Fatal(err)
		}
		_, err := s.SignRecord(ctx, SignInput{Entity: "non-conformances", BusinessID: other, Meaning: "closed",
			Reason: "replay", Signer: signer("close")})
		if !errors.Is(err, ErrConflict) {
			t.Fatalf("replayed jti: want ErrConflict, got %v", err)
		}
		got, _ := s.GetNonConformance(ctx, other)
		if got.Status == "closed" {
			t.Fatal("a refused signature must not have closed the record (transaction rolled back)")
		}
	})

	t.Run("a release is approved by signature and then locked", func(t *testing.T) {
		rel := "REL-SIG-" + run
		if _, _, err := s.UpsertReleaseDecision(ctx, UpsertReleaseDecisionInput{BusinessID: rel, BatchRef: "BAT-SIG-" + run,
			Decision: str("released")}); err != nil {
			t.Fatal(err)
		}
		sig, err := s.SignRecord(ctx, SignInput{Entity: "release-decisions", BusinessID: rel, Meaning: "approved",
			Reason: "all checks passed", Signer: signer("rel")})
		if err != nil || sig.Meaning != "approved" {
			t.Fatalf("sign release: %+v %v", sig, err)
		}
		_, _, err = s.UpsertReleaseDecision(ctx, UpsertReleaseDecisionInput{BusinessID: rel, BatchRef: "BAT-SIG-" + run,
			Decision: str("hold"), ChangeReason: "changed my mind"})
		if !errors.Is(err, ErrLocked) {
			t.Fatalf("a signed release is locked: got %v", err)
		}
	})

	t.Run("signatures are append-only", func(t *testing.T) {
		_, err := pool.Exec(ctx, `UPDATE qc_signatures SET signer_email = 'mallory' WHERE id = $1`, closing.ID)
		if err == nil || !strings.Contains(err.Error(), "append-only") {
			t.Fatalf("editing a signature must be refused, got %v", err)
		}
	})

	t.Run("a spec approval is recorded without locking the spec", func(t *testing.T) {
		if _, err := s.SignRecord(ctx, SignInput{Entity: "specifications", BusinessID: "SPEC-SEED-0001", Meaning: "approved",
			Reason: "annual review", Signer: signer("spec")}); err != nil {
			t.Fatal(err)
		}
		if locked, _ := s.RecordLocked(ctx, "specifications", "SPEC-SEED-0001"); locked {
			t.Fatal("approving a spec does not freeze it; the next version is approved in turn")
		}
	})
}
