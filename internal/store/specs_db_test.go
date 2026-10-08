package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"iag-quality-control/backend/internal/db"
	"iag-quality-control/backend/internal/migrate"
)

/*
Specification limits and the auto actions they drive (016), against a real
Postgres. Env-gated like TestUpsertGuardsAgainstRealPostgres — see that test's
comment for how to point it at a throwaway database:

	QC_TEST_DATABASE_URL="postgres://postgres:pg@127.0.0.1:55433/postgres?sslmode=disable" \
	  go test ./internal/store/ -run TestSpecificationsAgainstRealPostgres -v

The SQL here is the kind that compiles and is wrong: a partial-index ON
CONFLICT that never matches raises a second NC on every save, and a hold
selected FROM an NC insert that did nothing must itself do nothing.
*/
func TestSpecificationsAgainstRealPostgres(t *testing.T) {
	dsn := os.Getenv("QC_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("QC_TEST_DATABASE_URL not set — see TestUpsertGuardsAgainstRealPostgres")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
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
	id := func(prefix string) string { return fmt.Sprintf("%s-TEST-%s", prefix, run) }
	str := func(v string) *string { return &v }
	num := func(v float64) *float64 { return &v }

	countHolds := func(t *testing.T, ref string) int {
		t.Helper()
		var n int
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM qc_hold_events WHERE batch_ref = $1`, ref).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	t.Run("seeded specs resolve most-specific first", func(t *testing.T) {
		incoming, err := s.ResolveSpec(ctx, "incoming", "Moisture %", "")
		if err != nil || incoming.BusinessID != "SPEC-SEED-0002" || incoming.ActionOnFail != "hold" {
			t.Fatalf("incoming moisture: %+v, %v", incoming, err)
		}
		anyStage, err := s.ResolveSpec(ctx, "", "moisture_pct", "")
		if err != nil || anyStage.BusinessID != "SPEC-SEED-0001" {
			t.Fatalf("stageless moisture should fall back to the 'any' spec: %+v, %v", anyStage, err)
		}
		if _, err := s.ResolveSpec(ctx, "", "caffeine", ""); !errors.Is(err, ErrNotFound) {
			t.Fatalf("no spec for caffeine yet, got %v", err)
		}
	})

	gradeParam := "caffeine_" + run[len(run)-3:]
	t.Run("a grade-specific spec beats the all-grades one", func(t *testing.T) {
		if _, err := s.UpsertSpecification(ctx, UpsertSpecificationInput{
			BusinessID: id("SPA"), Parameter: gradeParam, Stage: str("finished"), USL: str("1.5"),
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.UpsertSpecification(ctx, UpsertSpecificationInput{
			BusinessID: id("SPB"), Parameter: gradeParam, Stage: str("finished"), Grade: str("AA"), USL: str("1.2"),
		}); err != nil {
			t.Fatal(err)
		}
		got, err := s.ResolveSpec(ctx, "finished", gradeParam, "AA")
		if err != nil || got.BusinessID != id("SPB") {
			t.Fatalf("AA should pick its own spec: %+v %v", got, err)
		}
		got, err = s.ResolveSpec(ctx, "finished", gradeParam, "AB")
		if err != nil || got.BusinessID != id("SPA") {
			t.Fatalf("AB should fall back to all grades: %+v %v", got, err)
		}
	})

	t.Run("a second active spec for a slot is a conflict, a bad band is bad input", func(t *testing.T) {
		_, err := s.UpsertSpecification(ctx, UpsertSpecificationInput{
			BusinessID: id("SPC"), Parameter: gradeParam, Stage: str("finished"), USL: str("2"),
		})
		if !errors.Is(err, ErrConflict) {
			t.Fatalf("want ErrConflict, got %v", err)
		}
		_, err = s.UpsertSpecification(ctx, UpsertSpecificationInput{
			BusinessID: id("SPD"), Parameter: "other_" + run, LSL: str("5"), USL: str("1"),
		})
		if !errors.Is(err, ErrBadInput) {
			t.Fatalf("LSL above USL: want ErrBadInput, got %v", err)
		}
		// Deactivating frees the slot, and nil keeps the limit it did not mention.
		off := false
		retired, err := s.UpsertSpecification(ctx, UpsertSpecificationInput{BusinessID: id("SPA"), Parameter: gradeParam, Active: &off})
		if err != nil || retired.Active || retired.USL == nil || *retired.USL != 1.5 {
			t.Fatalf("retire: %+v %v", retired, err)
		}
		if _, err := s.UpsertSpecification(ctx, UpsertSpecificationInput{
			BusinessID: id("SPC"), Parameter: gradeParam, Stage: str("finished"), USL: str("2"),
		}); err != nil {
			t.Fatalf("slot should be free after retiring: %v", err)
		}
	})

	batch := id("BAT")
	sample, err := s.CreateSample(ctx, CreateSampleInput{BatchBusinessID: batch, SampleType: "green", SampleID: id("SMP")})
	if err != nil {
		t.Fatalf("sample: %v", err)
	}

	t.Run("a failing measurement on a flag spec raises an NC and no hold", func(t *testing.T) {
		m, err := s.CreateLabMeasurement(ctx, CreateLabMeasurementInput{
			BusinessID: id("MSA"), SampleBusinessID: sample.BusinessID, Parameter: "Moisture %", Value: "14.1",
		})
		if err != nil {
			t.Fatal(err)
		}
		if m.Verdict != "fail" || m.Result != "fail" || len(m.Evaluation) != 1 || m.Evaluation[0].SpecID != "SPEC-SEED-0001" {
			t.Fatalf("verdict/result/evaluation: %+v", m)
		}
		if m.AutoActions == nil || !m.AutoActions.Created || m.AutoActions.NonConformanceID == "" || m.AutoActions.HoldEventID != "" {
			t.Fatalf("flag spec should raise an NC only: %+v", m.AutoActions)
		}
		nc, err := s.GetNonConformance(ctx, m.AutoActions.NonConformanceID)
		if err != nil || nc.SourceRef != m.BusinessID || nc.Severity != "minor" {
			t.Fatalf("nc: %+v %v", nc, err)
		}
		if countHolds(t, batch) != 0 {
			t.Fatal("flag spec must not hold")
		}
	})

	t.Run("a result contradicting the verdict needs a reason", func(t *testing.T) {
		_, err := s.CreateLabMeasurement(ctx, CreateLabMeasurementInput{
			BusinessID: id("MSB"), SampleBusinessID: sample.BusinessID, Parameter: "moisture", Value: "13", Result: "pass",
		})
		if !errors.Is(err, ErrNeedsOverride) {
			t.Fatalf("want ErrNeedsOverride, got %v", err)
		}
		m, err := s.CreateLabMeasurement(ctx, CreateLabMeasurementInput{
			BusinessID: id("MSB"), SampleBusinessID: sample.BusinessID, Parameter: "moisture", Value: "13",
			Result: "pass", OverrideReason: "Meter drifted; re-tested on the reference meter at 12.1",
		})
		if err != nil || m.Result != "pass" || m.Verdict != "fail" || m.OverrideReason == "" {
			t.Fatalf("override: %+v %v", m, err)
		}
		pass, err := s.CreateLabMeasurement(ctx, CreateLabMeasurementInput{
			BusinessID: id("MSC"), SampleBusinessID: sample.BusinessID, Parameter: "moisture", Value: "11.2",
		})
		if err != nil || pass.Verdict != "pass" || pass.Result != "pass" || pass.AutoActions != nil {
			t.Fatalf("pass: %+v %v", pass, err)
		}
		none, err := s.CreateLabMeasurement(ctx, CreateLabMeasurementInput{
			BusinessID: id("MSD"), SampleBusinessID: sample.BusinessID, Parameter: "pH", Value: "5.1", Result: "pending",
		})
		if err != nil || none.Verdict != "no_spec" || none.Result != "pending" {
			t.Fatalf("no spec: %+v %v", none, err)
		}
	})

	lot := id("LOT")
	t.Run("a wet lot on receipt is quarantined, once", func(t *testing.T) {
		insp, err := s.UpsertIncomingInspection(ctx, UpsertIncomingInspectionInput{
			BusinessID: id("IIN"), SourceLot: lot, MoisturePct: num(13.4), Inspector: str("QA"),
		})
		if err != nil {
			t.Fatal(err)
		}
		if insp.Verdict != "fail" || insp.Result != "quarantine" || insp.Status != "hold" {
			t.Fatalf("quarantine: %+v", insp)
		}
		if insp.AutoActions == nil || insp.AutoActions.HoldEventID == "" || insp.AutoActions.HoldRef != lot {
			t.Fatalf("hold: %+v", insp.AutoActions)
		}
		// A notes-only edit keeps the verdict (the stored moisture is re-judged)
		// and raises nothing new.
		again, err := s.UpsertIncomingInspection(ctx, UpsertIncomingInspectionInput{
			BusinessID: id("IIN"), SourceLot: lot, Notes: str("bags resealed"),
		})
		if err != nil {
			t.Fatal(err)
		}
		if again.Verdict != "fail" || again.AutoActions == nil || again.AutoActions.Created ||
			again.AutoActions.NonConformanceID != insp.AutoActions.NonConformanceID {
			t.Fatalf("re-save should find the same NC and create nothing: %+v", again.AutoActions)
		}
		if n := countHolds(t, lot); n != 1 {
			t.Fatalf("want exactly one hold on the lot, got %d", n)
		}
		// Accepting it anyway needs a reason; corrected moisture passes.
		if _, err := s.UpsertIncomingInspection(ctx, UpsertIncomingInspectionInput{
			BusinessID: id("IIN"), SourceLot: lot, Result: str("accept"),
		}); !errors.Is(err, ErrNeedsOverride) {
			t.Fatalf("want ErrNeedsOverride, got %v", err)
		}
		fixed, err := s.UpsertIncomingInspection(ctx, UpsertIncomingInspectionInput{
			BusinessID: id("IIN"), SourceLot: lot, MoisturePct: num(11.9),
		})
		if err != nil || fixed.Verdict != "pass" || fixed.Result != "accept" {
			t.Fatalf("corrected reading: %+v %v", fixed, err)
		}
	})

	ipcParam := "drop_temp_" + run[len(run)-3:]
	t.Run("an in-process check out of band holds the batch", func(t *testing.T) {
		if _, err := s.UpsertSpecification(ctx, UpsertSpecificationInput{
			BusinessID: id("SPE"), Parameter: ipcParam, Stage: str("in_process"),
			LSL: str("185"), USL: str("235"), Unit: str("°C"), ActionOnFail: str("hold"),
		}); err != nil {
			t.Fatal(err)
		}
		ipcBatch := id("BIP")
		chk, err := s.UpsertInProcessCheck(ctx, UpsertInProcessCheckInput{
			BusinessID: id("IPC"), Parameter: ipcParam, Stage: str("Roasting"), BatchRef: &ipcBatch, Actual: str("241"),
		})
		if err != nil {
			t.Fatal(err)
		}
		if chk.Verdict != "fail" || chk.Result != "out_of_control" || chk.AutoActions == nil || chk.AutoActions.HoldEventID == "" {
			t.Fatalf("ipc: %+v / %+v", chk, chk.AutoActions)
		}
		if countHolds(t, ipcBatch) != 1 {
			t.Fatal("want one hold on the batch")
		}
	})
}
