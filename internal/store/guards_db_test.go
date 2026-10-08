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
The one test in this package that talks to a real database.

Everything else here is pure-function, because there is no Postgres in CI — which
left the most consequential SQL in the service unverified by anything. The upsert
guards are exactly the kind of change that compiles, runs, and quietly does the
wrong thing: `COALESCE(EXCLUDED.col, t.col)` preserves nothing, a missing ::text
cast fails only at run time, and a mirror written the wrong way round silently
empties the calibration calendar. None of that is visible without executing it.

Skipped unless QC_TEST_DATABASE_URL is set, following the env-gated pattern the
permission-catalogue drift check already uses. Point it at a throwaway database —
it applies every migration and writes rows:

	docker run -d --name qc-test -e POSTGRES_PASSWORD=pg -p 55433:5432 postgres:16-alpine
	QC_TEST_DATABASE_URL="postgres://postgres:pg@127.0.0.1:55433/postgres?sslmode=disable" \
	  go test ./internal/store/ -run TestUpsertGuardsAgainstRealPostgres -v

Business ids are suffixed per run so a repeated run against the same database
does not collide with its own leftovers.
*/
func TestUpsertGuardsAgainstRealPostgres(t *testing.T) {
	dsn := os.Getenv("QC_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("QC_TEST_DATABASE_URL not set — see the comment above for how to run this")
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

	t.Run("instrument keeps the columns a partial save omitted", func(t *testing.T) {
		ins := id("INS")
		if _, err := s.UpsertInstrument(ctx, UpsertInstrumentInput{
			BusinessID: ins, Name: "Moisture meter",
			InstrumentType: ptr("moisture"), Location: ptr("Lab bench 2"), OwnerTech: ptr("TEC-1"),
			LastCalDate: ptr("2026-06-01"), NextCalDate: ptr("2026-12-01"),
			Note: ptr("ref A"), Samples24h: intPtr(42), MESAssetTag: ptr("MES-77"),
		}); err != nil {
			t.Fatalf("create: %v", err)
		}
		got, err := s.UpsertInstrument(ctx, UpsertInstrumentInput{BusinessID: ins, Name: "Renamed"})
		if err != nil {
			t.Fatalf("partial save: %v", err)
		}
		if got.Name != "Renamed" {
			t.Errorf("name not updated: %q", got.Name)
		}
		// The telemetry columns are the ones a form save used to destroy.
		if got.Location != "Lab bench 2" || got.MESAssetTag != "MES-77" || got.Samples24h != 42 {
			t.Errorf("partial save blanked a column: location=%q tag=%q samples=%d",
				got.Location, got.MESAssetTag, got.Samples24h)
		}
		if got.LastCalDate == nil || got.NextCalDate == nil {
			t.Fatal("partial save NULLed the calibration dates — this drops the instrument off the calendar")
		}

		cleared, err := s.UpsertInstrument(ctx, UpsertInstrumentInput{
			BusinessID: ins, Name: "Renamed", Location: ptr(""), NextCalDate: ptr(""),
		})
		if err != nil {
			t.Fatalf("clearing save: %v", err)
		}
		if cleared.Location != "" || cleared.NextCalDate != nil {
			t.Errorf(`explicit "" did not clear: location=%q next=%v`, cleared.Location, cleared.NextCalDate)
		}
		if cleared.Note != "ref A" {
			t.Errorf("clearing one column cleared another: note=%q", cleared.Note)
		}
	})

	t.Run("CAPA is not reopened by a partial save", func(t *testing.T) {
		capa := id("CAPA")
		if _, err := s.UpsertCAPA(ctx, UpsertCAPAInput{
			BusinessID: capa, Title: "Retrain intake", Status: ptr("closed"),
			RootCause: ptr("missed step"), OpenedAt: ptr("2026-05-01"),
		}); err != nil {
			t.Fatalf("create: %v", err)
		}
		got, err := s.UpsertCAPA(ctx, UpsertCAPAInput{
			BusinessID: capa, Title: "Retrain intake", Owner: ptr("B. Owner"),
		})
		if err != nil {
			t.Fatalf("partial save: %v", err)
		}
		if got.Status != "closed" {
			t.Errorf("status reopened to %q", got.Status)
		}
		if got.RootCause != "missed step" || got.OpenedAt != "2026-05-01" {
			t.Errorf("partial save blanked a column: root=%q opened=%q", got.RootCause, got.OpenedAt)
		}
	})

	t.Run("technician is not reactivated by a partial save", func(t *testing.T) {
		tec := id("TEC")
		if _, err := s.UpsertTechnician(ctx, UpsertTechnicianInput{
			BusinessID: tec, Name: "Stood down", Role: ptr("cupper"),
			Certifications: []string{"Q-grader"}, Active: boolPtr(false),
		}); err != nil {
			t.Fatalf("create: %v", err)
		}
		got, err := s.UpsertTechnician(ctx, UpsertTechnicianInput{BusinessID: tec, Name: "Renamed"})
		if err != nil {
			t.Fatalf("partial save: %v", err)
		}
		if got.Active {
			t.Error("partial save reactivated a technician who had been stood down")
		}
		if got.Role != "cupper" || len(got.Certifications) != 1 {
			t.Errorf("partial save blanked a column: role=%q certs=%v", got.Role, got.Certifications)
		}
	})

	t.Run("lab method attrs merge rather than replace", func(t *testing.T) {
		lm := id("LM")
		if _, err := s.UpsertLabMethod(ctx, UpsertLabMethodInput{
			BusinessID: lm, Name: "Moisture by oven", Scope: ptr("green coffee"),
			Attrs: map[string]any{"attachments": "sop.pdf"},
		}); err != nil {
			t.Fatalf("create: %v", err)
		}
		got, err := s.UpsertLabMethod(ctx, UpsertLabMethodInput{
			BusinessID: lm, Name: "Moisture by oven",
			Attrs: map[string]any{"reviewer": "QA lead"},
		})
		if err != nil {
			t.Fatalf("partial save: %v", err)
		}
		if got.Scope != "green coffee" {
			t.Errorf("scope blanked: %q", got.Scope)
		}
		if got.Attrs["attachments"] != "sop.pdf" {
			t.Error("a second writer wiped the first writer's attrs key")
		}
		if got.Attrs["reviewer"] != "QA lead" {
			t.Error("the new attrs key was not merged in")
		}
	})

	t.Run("calibration mirrors onto the register, latest event wins", func(t *testing.T) {
		ins := id("INSCAL")
		if _, err := s.UpsertInstrument(ctx, UpsertInstrumentInput{BusinessID: ins, Name: "Meter"}); err != nil {
			t.Fatalf("create instrument: %v", err)
		}
		cal, err := s.CreateInstrumentCalibration(ctx, CreateCalibrationInput{
			InstrumentID: ins, CalDate: "2026-09-20", NextDue: "2027-03-20",
			Result: "Pass", Status: "Current",
		})
		if err != nil {
			t.Fatalf("create calibration: %v", err)
		}
		if cal.Result != "pass" || cal.Status != "current" {
			t.Errorf("vocabulary not normalised: result=%q status=%q", cal.Result, cal.Status)
		}
		reg, err := s.GetInstrument(ctx, ins)
		if err != nil {
			t.Fatalf("get instrument: %v", err)
		}
		if reg.LastCalDate == nil || *reg.LastCalDate != "2026-09-20" ||
			reg.NextCalDate == nil || *reg.NextCalDate != "2027-03-20" {
			t.Fatalf("register not mirrored: last=%v next=%v", reg.LastCalDate, reg.NextCalDate)
		}

		// Back-filling history must not drag the due date backwards, which would
		// silently empty the calibration calendar.
		if _, err := s.CreateInstrumentCalibration(ctx, CreateCalibrationInput{
			InstrumentID: ins, CalDate: "2025-01-15", NextDue: "2025-07-15", Result: "Adjust",
		}); err != nil {
			t.Fatalf("back-dated calibration: %v", err)
		}
		after, err := s.GetInstrument(ctx, ins)
		if err != nil {
			t.Fatalf("get instrument: %v", err)
		}
		if *after.LastCalDate != "2026-09-20" || *after.NextCalDate != "2027-03-20" {
			t.Errorf("a back-dated event moved the register: last=%v next=%v",
				*after.LastCalDate, *after.NextCalDate)
		}
		hist, err := s.ListInstrumentCalibrations(ctx, ins, "", 0)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(hist) != 2 {
			t.Errorf("history kept %d events, want 2", len(hist))
		}

		// An instrument the register has never heard of is still worth recording.
		orphan, err := s.CreateInstrumentCalibration(ctx, CreateCalibrationInput{
			InstrumentName: "Borrowed refractometer", CalDate: "2026-09-21", Result: "Fail",
		})
		if err != nil {
			t.Fatalf("unregistered instrument: %v", err)
		}
		if orphan.InstrumentName != "Borrowed refractometer" {
			t.Errorf("unregistered calibration lost its label: %q", orphan.InstrumentName)
		}
	})

	t.Run("release decision announces once, not on every edit", func(t *testing.T) {
		rel := id("REL")
		_, emit, err := s.UpsertReleaseDecision(ctx, UpsertReleaseDecisionInput{
			BusinessID: rel, BatchRef: "BATCH-1", Product: ptr("Arabica AA"),
			Decision: ptr("Released"), DecidedBy: ptr("QA lead"),
		})
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if !emit {
			t.Fatal("a new released decision must be announced")
		}

		// Editing anything else must NOT re-announce: warehouse and traceability
		// act on these events, and a batch is released once.
		got, emit, err := s.UpsertReleaseDecision(ctx, UpsertReleaseDecisionInput{
			BusinessID: rel, BatchRef: "BATCH-1", Notes: ptr("typo fixed"),
		})
		if err != nil {
			t.Fatalf("edit: %v", err)
		}
		if emit {
			t.Error("editing the notes re-announced the release")
		}
		if got.Decision != "released" {
			t.Errorf("decision not preserved by a partial save: %q", got.Decision)
		}
		if got.Product != "Arabica AA" {
			t.Errorf("product blanked by a partial save: %q", got.Product)
		}

		// Changing the decision must announce again, with the other event type.
		changed, emit, err := s.UpsertReleaseDecision(ctx, UpsertReleaseDecisionInput{
			BusinessID: rel, BatchRef: "BATCH-1", Decision: ptr("Hold"),
		})
		if err != nil {
			t.Fatalf("change decision: %v", err)
		}
		if !emit {
			t.Error("changing the decision must be announced")
		}
		if ReleaseEventType(changed.Decision) != "qc.batch.held" {
			t.Errorf("wrong event for %q: %s", changed.Decision, ReleaseEventType(changed.Decision))
		}
	})

	t.Run("hold log refuses to rewrite an existing reference", func(t *testing.T) {
		hld := id("HLD")
		if _, err := s.CreateHoldEvent(ctx, CreateHoldEventInput{
			BusinessID: hld, BatchRef: "BATCH-1", Event: "Placed on hold", Reason: "moisture high",
		}); err != nil {
			t.Fatalf("create: %v", err)
		}
		// An audit log that can be rewritten is not one.
		if _, err := s.CreateHoldEvent(ctx, CreateHoldEventInput{
			BusinessID: hld, BatchRef: "BATCH-1", Event: "Released", Reason: "overwritten",
		}); !errors.Is(err, ErrConflict) {
			t.Fatalf("re-posting the same reference gave %v, want ErrConflict", err)
		}
	})

	t.Run("non-conformance keeps the columns a partial save omitted", func(t *testing.T) {
		ncr := id("NCR")
		if _, err := s.UpsertNonConformance(ctx, UpsertNonConformanceInput{
			BusinessID: ncr, Title: "Foreign matter", Severity: ptr("major"),
			Owner: ptr("A. Owner"), Description: ptr("found in sample"), RootCause: ptr("screen torn"),
			Status: ptr("under_investigation"),
		}); err != nil {
			t.Fatalf("create: %v", err)
		}
		got, err := s.UpsertNonConformance(ctx, UpsertNonConformanceInput{
			BusinessID: ncr, Title: "Foreign matter (confirmed)",
		})
		if err != nil {
			t.Fatalf("partial save: %v", err)
		}
		if got.Severity != "major" || got.RootCause != "screen torn" || got.Status != "under_investigation" {
			t.Errorf("partial save blanked a column: sev=%q root=%q status=%q",
				got.Severity, got.RootCause, got.Status)
		}
	})

	t.Run("CAPA carries the four fields the form used to drop", func(t *testing.T) {
		capa := id("CAPAX")
		got, err := s.UpsertCAPA(ctx, UpsertCAPAInput{
			BusinessID: capa, Title: "Replace screen", DueDate: ptr("2026-11-30"),
			CAPAKind: ptr("Corrective"), Effectiveness: ptr("re-inspect after 30 days"),
			Attachments: ptr("[]"),
		})
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if got.DueDate == nil || *got.DueDate != "2026-11-30" {
			t.Errorf("due_date not stored: %v", got.DueDate)
		}
		if got.CAPAKind != "Corrective" || got.Effectiveness == "" {
			t.Errorf("capa_kind/effectiveness not stored: %q / %q", got.CAPAKind, got.Effectiveness)
		}
	})

	t.Run("a measurement records any analyte and mirrors only the known ones", func(t *testing.T) {
		batch := "BATCH-" + run
		sample, err := s.CreateSample(ctx, CreateSampleInput{
			BatchBusinessID: batch, SampleID: id("SMP"), SampleType: "green",
		})
		if err != nil {
			t.Fatalf("create sample: %v", err)
		}

		// The case the table exists for: before it, this was stored as
		// moisture_pct and read back labelled "moisture".
		caffeine, err := s.CreateLabMeasurement(ctx, CreateLabMeasurementInput{
			SampleBusinessID: sample.BusinessID, Parameter: "caffeine",
			Value: "1.2", Unit: "mg/g", SpecLimit: "0.8-1.5", Analyst: "A. Analyst",
		})
		if err != nil {
			t.Fatalf("caffeine measurement: %v", err)
		}
		if caffeine.Parameter != "caffeine" || caffeine.Unit != "mg/g" {
			t.Errorf("analyte not stored as itself: %q %q", caffeine.Parameter, caffeine.Unit)
		}
		if caffeine.ValueNum == nil || *caffeine.ValueNum != 1.2 {
			t.Errorf("numeric value not parsed: %v", caffeine.ValueNum)
		}
		if caffeine.BatchBusinessID != batch {
			t.Errorf("batch not resolved from the sample: %q", caffeine.BatchBusinessID)
		}
		// An unknown analyte must not touch the rollup.
		if summary, err := s.GetBatchLabSummary(ctx, batch); err == nil && summary.Moisture != nil {
			t.Errorf("caffeine leaked into the moisture rollup: %v", *summary.Moisture)
		}

		// A known metric must still feed the rollup, or SPC, the dashboard and
		// the CoA PDF quietly stop being fed.
		if _, err := s.CreateLabMeasurement(ctx, CreateLabMeasurementInput{
			SampleBusinessID: sample.BusinessID, Parameter: "Moisture %", Value: "11.5", Unit: "%",
		}); err != nil {
			t.Fatalf("moisture measurement: %v", err)
		}
		summary, err := s.GetBatchLabSummary(ctx, batch)
		if err != nil {
			t.Fatalf("rollup: %v", err)
		}
		if summary.Moisture == nil || *summary.Moisture != 11.5 {
			t.Errorf("moisture did not reach the rollup: %v", summary.Moisture)
		}

		// A non-numeric result is still a result.
		trace, err := s.CreateLabMeasurement(ctx, CreateLabMeasurementInput{
			SampleBusinessID: sample.BusinessID, Parameter: "ochratoxin", Value: "<0.1",
		})
		if err != nil {
			t.Fatalf("non-numeric measurement: %v", err)
		}
		if trace.ValueText != "<0.1" || trace.ValueNum != nil {
			t.Errorf("non-numeric value mishandled: text=%q num=%v", trace.ValueText, trace.ValueNum)
		}

		list, err := s.ListLabMeasurements(ctx, sample.BusinessID, "", 0)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(list) != 3 {
			t.Errorf("listed %d measurements, want 3", len(list))
		}
	})

	t.Run("sample attrs merge, so notes can go back to being notes", func(t *testing.T) {
		created, err := s.CreateSample(ctx, CreateSampleInput{
			BatchBusinessID: "BATCH-ATTRS-" + run, SampleID: id("SMPA"),
			Notes: "arrived warm",
			Attrs: map[string]any{"location": "Cold room A", "quantity": "500", "unit": "g"},
		})
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if created.Notes != "arrived warm" {
			t.Errorf("notes should carry only notes now: %q", created.Notes)
		}
		if created.Attrs["location"] != "Cold room A" {
			t.Errorf("attrs not stored: %v", created.Attrs)
		}

		// A later edit that knows about attachments must not wipe the intake
		// fields, and must not have to resend them.
		patched, err := s.UpdateSample(ctx, created.BusinessID, SamplePatch{
			Attrs: map[string]any{"attachments": "[{\"storageId\":\"abc\"}]"},
		})
		if err != nil {
			t.Fatalf("patch: %v", err)
		}
		if patched.Attrs["location"] != "Cold room A" {
			t.Error("a second writer wiped the first writer's attrs keys")
		}
		if patched.Attrs["attachments"] == nil {
			t.Error("attachments did not merge in")
		}
		if patched.Notes != "arrived warm" {
			t.Errorf("an attrs-only patch changed the notes: %q", patched.Notes)
		}
	})

	t.Run("stability study keeps the columns a partial save omitted", func(t *testing.T) {
		stb := id("STB")
		if _, err := s.UpsertStabilityStudy(ctx, UpsertStabilityStudyInput{
			BusinessID: stb, Product: "Arabica AA", StorageCondition: ptr("30C/65RH"),
			DurationDays: intPtr(180), NextPullDate: ptr("2026-10-01"), Status: ptr("running"),
		}); err != nil {
			t.Fatalf("create: %v", err)
		}
		got, err := s.UpsertStabilityStudy(ctx, UpsertStabilityStudyInput{
			BusinessID: stb, Product: "Arabica AA", Owner: ptr("B. Owner"),
		})
		if err != nil {
			t.Fatalf("partial save: %v", err)
		}
		if got.StorageCondition != "30C/65RH" || got.DurationDays != 180 || got.Status != "running" {
			t.Errorf("partial save blanked a column: cond=%q days=%d status=%q",
				got.StorageCondition, got.DurationDays, got.Status)
		}
		if got.NextPullDate == nil || *got.NextPullDate != "2026-10-01" {
			t.Errorf("next_pull_date not preserved: %v", got.NextPullDate)
		}
	})
}

func intPtr(n int) *int    { return &n }
func boolPtr(v bool) *bool { return &v }
