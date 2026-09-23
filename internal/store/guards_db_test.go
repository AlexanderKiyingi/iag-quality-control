package store

import (
	"context"
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
