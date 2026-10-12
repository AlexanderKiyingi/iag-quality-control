package store

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"iag-quality-control/backend/internal/db"
	"iag-quality-control/backend/internal/migrate"
)

// AQL sampling and stability (024) against a real Postgres. Env-gated; see
// TestUpsertGuardsAgainstRealPostgres.
func TestSamplingAndStabilityAgainstRealPostgres(t *testing.T) {
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
	num := func(v int) *int { return &v }
	f := func(v float64) *float64 { return &v }

	t.Run("an AQL lot gets its plan and is judged by it", func(t *testing.T) {
		ok, err := s.UpsertIncomingInspection(ctx, UpsertIncomingInspectionInput{SourceLot: "AQL-OK-" + run,
			LotSize: num(1000), InspectionLevel: str("II"), AQL: f(2.5), DefectCount: num(5), Actor: "ana@iag"})
		if err != nil {
			t.Fatal(err)
		}
		if ok.SampleSize != 80 || ok.AcceptNumber == nil || *ok.AcceptNumber != 5 || ok.Verdict != "pass" || ok.InspectionLevel != "II" {
			t.Fatalf("5 of 80 at AQL 2.5: %+v", ok)
		}
		bad, err := s.UpsertIncomingInspection(ctx, UpsertIncomingInspectionInput{SourceLot: "AQL-BAD-" + run,
			LotSize: num(1000), AQL: f(2.5), DefectCount: num(6), Actor: "ana@iag"})
		if err != nil {
			t.Fatal(err)
		}
		if bad.Verdict != "fail" || bad.AutoActions == nil || bad.AutoActions.NonConformanceID == "" {
			t.Fatalf("6 of 80 fails and raises an NC: %+v", bad)
		}
		if _, err := s.UpsertIncomingInspection(ctx, UpsertIncomingInspectionInput{SourceLot: "AQL-X-" + run,
			LotSize: num(1000), AQL: f(3.0), Actor: "ana@iag"}); !errors.Is(err, ErrBadInput) {
			t.Fatalf("AQL 3.0 is not a column: %v", err)
		}
	})

	study := "STB-" + run
	start := time.Now().AddDate(0, 0, -100).Format("2006-01-02")
	if _, err := s.UpsertStabilityStudy(ctx, UpsertStabilityStudyInput{BusinessID: study, Product: "Roasted beans 250 g",
		StartDate: str(start), StorageCondition: str("25°C / 60% RH")}); err != nil {
		t.Fatal(err)
	}

	t.Run("timepoints become pull points; the study mirrors the next one", func(t *testing.T) {
		pulls, err := s.SchedulePullPoints(ctx, study, []int{0, 30, 60, 90, 180, 30})
		if err != nil || len(pulls) != 5 || pulls[4].PullDay != 180 {
			t.Fatalf("schedule: %+v %v", pulls, err)
		}
		if !pulls[1].Overdue {
			t.Fatalf("day 30 of a study started 100 days ago is overdue: %+v", pulls[1])
		}
		// Rescheduling without day 180 cancels it rather than deleting it.
		pulls, _ = s.SchedulePullPoints(ctx, study, []int{0, 30, 60, 90})
		if pulls[4].Status != "cancelled" {
			t.Fatalf("dropped point: %+v", pulls[4])
		}
		if _, err := s.SchedulePullPoints(ctx, "STB-NOPE", []int{0}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("unknown study: %v", err)
		}
	})

	t.Run("results mark a pull tested and feed the shelf-life estimate", func(t *testing.T) {
		for day, v := range map[int]float64{0: 10.0, 30: 10.4, 60: 10.9, 90: 11.3} {
			if _, err := s.RecordStabilityResults(ctx, study, day, []StabilityReading{{Parameter: "Moisture %", Value: v, Unit: "%"}}, "", "ana@iag"); err != nil {
				t.Fatal(err)
			}
		}
		pulls, _ := s.ListPullPoints(ctx, study)
		if pulls[0].Status != "tested" || len(pulls[0].Results) != 1 || pulls[0].Results[0].Parameter != "moisture" {
			t.Fatalf("tested pull: %+v", pulls[0])
		}
		if _, err := s.RecordStabilityResults(ctx, study, 180, []StabilityReading{{Parameter: "moisture", Value: 12}}, "", "ana@iag"); !errors.Is(err, ErrConflict) {
			t.Fatalf("a cancelled pull takes no results: %v", err)
		}
		if _, err := s.RecordStabilityResults(ctx, study, 45, []StabilityReading{{Parameter: "moisture", Value: 12}}, "", "ana@iag"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("an unscheduled day: %v", err)
		}
		est, _, err := s.StabilityShelfLife(ctx, study, "moisture", "", nil, f(12.5))
		if err != nil || est.N != 4 || est.Side != "upper" || est.Days == nil || *est.Days < 120 || *est.Days > 180 {
			t.Fatalf("shelf life: %+v %v", est, err)
		}
	})

	t.Run("a due pull is reminded once", func(t *testing.T) {
		if _, err := s.SchedulePullPoints(ctx, study, []int{0, 30, 60, 90, 101}); err != nil {
			t.Fatal(err)
		}
		due, err := s.DueStabilityPulls(ctx, 3, true)
		found := false
		for _, d := range due {
			found = found || (d.StudyBusinessID == study && d.PullDay == 101)
		}
		if err != nil || !found {
			t.Fatalf("day 101 is due: %+v %v", due, err)
		}
		if err := s.MarkPullReminded(ctx, study, 101); err != nil {
			t.Fatal(err)
		}
		due, _ = s.DueStabilityPulls(ctx, 3, true)
		for _, d := range due {
			if d.StudyBusinessID == study && d.PullDay == 101 {
				t.Fatal("reminded twice")
			}
		}
	})
}
