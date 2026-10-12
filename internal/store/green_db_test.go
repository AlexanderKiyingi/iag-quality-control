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

// Green grading (021) against a real Postgres. Env-gated; see
// TestUpsertGuardsAgainstRealPostgres.
func TestGreenGradingAgainstRealPostgres(t *testing.T) {
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
	f := func(v float64) *float64 { return &v }
	q := func(v int) *int { return &v }
	target := "SCREEN18-" + run

	sample, err := s.CreateSample(ctx, CreateSampleInput{BatchBusinessID: "BAT-GRN-" + run, SampleID: "SMP-GRN-" + run})
	if err != nil {
		t.Fatal(err)
	}
	// The export grade is a spec on a derived metric: at least 90% above screen 18.
	if _, err := s.UpsertSpecification(ctx, UpsertSpecificationInput{BusinessID: "SPG-" + run,
		Parameter: "screen_18_plus_pct", Stage: str("incoming"), Grade: str(target), LSL: str("90"),
		ActionOnFail: str("hold"), Actor: "ana@iag"}); err != nil {
		t.Fatal(err)
	}

	t.Run("a clean lot grades specialty and passes its export spec", func(t *testing.T) {
		g, err := s.CreateGreenGrading(ctx, CreateGreenGradingInput{SampleBusinessID: sample.BusinessID,
			Defects: map[string]int{"broken": 12, "floater": 4}, Screens: map[string]float64{"18": 330, "17": 15, "pan": 5},
			MoisturePct: f(10.8), Quakers: q(0), GradeTarget: target, Actor: "ana@iag"})
		if err != nil {
			t.Fatal(err)
		}
		if g.SCAGrade != "specialty" || g.FullDefects != 2 || g.Verdict != "pass" || g.AutoActions != nil {
			t.Fatalf("clean lot: grade %s full %v verdict %s auto %+v", g.SCAGrade, g.FullDefects, g.Verdict, g.AutoActions)
		}
		if g.Result.ScreenPlusPct["18"] != 94.3 || g.Grader != "ana@iag" || g.Stage != "incoming" {
			t.Fatalf("result: %+v grader %q stage %q", g.Result.ScreenPlusPct, g.Grader, g.Stage)
		}
	})

	var failing GreenGrading
	t.Run("a small-bean lot fails its spec, raising an NC and a hold", func(t *testing.T) {
		failing, err = s.CreateGreenGrading(ctx, CreateGreenGradingInput{SampleBusinessID: sample.BusinessID,
			Defects: map[string]int{"full_black": 2}, Screens: map[string]float64{"18": 200, "16": 150},
			MoisturePct: f(11.4), GradeTarget: target, Actor: "ana@iag"})
		if err != nil {
			t.Fatal(err)
		}
		if failing.Verdict != "fail" || failing.AutoActions == nil || failing.AutoActions.NonConformanceID == "" ||
			failing.AutoActions.HoldEventID == "" {
			t.Fatalf("failing lot: verdict %s auto %+v", failing.Verdict, failing.AutoActions)
		}
		if failing.SCAGrade != "premium" {
			t.Fatalf("two full blacks rule out specialty: %s %v", failing.SCAGrade, failing.Result.GradeReasons)
		}
	})

	t.Run("gradings feed SPC and the batch rollup", func(t *testing.T) {
		series, err := s.SPC(ctx, SPCOptions{Metric: "screen_18_plus_pct", BatchID: sample.BatchBusinessID, Days: 7})
		if err != nil || series.Count != 2 {
			t.Fatalf("SPC: %+v %v", series, err)
		}
		params, err := s.SPCParameters(ctx, 7)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, p := range params {
			found = found || p.Parameter == "category1_full_defects"
		}
		if !found {
			t.Fatalf("grading metrics should be listed for SPC: %+v", params)
		}
		summary, err := s.GetBatchLabSummary(ctx, sample.BatchBusinessID)
		if err != nil || summary.Moisture == nil || *summary.Moisture != 11.4 || summary.Defects == nil || *summary.Defects != 2 {
			t.Fatalf("rollup: %+v %v", summary, err)
		}
	})

	t.Run("a wrong grading is voided with a reason, and leaves the lists", func(t *testing.T) {
		if _, err := s.VoidGreenGrading(ctx, failing.BusinessID, "ana@iag", ""); !errors.Is(err, ErrReasonRequired) {
			t.Fatalf("void without a reason: %v", err)
		}
		v, err := s.VoidGreenGrading(ctx, failing.BusinessID, "ana@iag", "screens were weighed wet")
		if err != nil || v.Status != "void" {
			t.Fatalf("void: %+v %v", v, err)
		}
		if _, err := s.VoidGreenGrading(ctx, failing.BusinessID, "ana@iag", "again"); !errors.Is(err, ErrConflict) {
			t.Fatalf("voiding twice: %v", err)
		}
		list, _ := s.ListGreenGradings(ctx, sample.BusinessID, "", false, 50)
		all, _ := s.ListGreenGradings(ctx, sample.BusinessID, "", true, 50)
		if len(list) != 1 || len(all) != 2 {
			t.Fatalf("lists: %d visible, %d with void", len(list), len(all))
		}
		log, err := s.ListChangeLog(ctx, "green-gradings", failing.BusinessID, "", 5)
		if err != nil || len(log) < 2 || log[0].Reason != "screens were weighed wet" || log[0].Entity != "green-gradings" {
			t.Fatalf("the void and its reason belong in the change log: %+v %v", log, err)
		}
	})

	t.Run("bad sheets are refused", func(t *testing.T) {
		if _, err := s.CreateGreenGrading(ctx, CreateGreenGradingInput{SampleBusinessID: sample.BusinessID,
			Defects: map[string]int{"mouldy": 3}}); !errors.Is(err, ErrBadInput) {
			t.Fatalf("unknown defect: %v", err)
		}
		if _, err := s.CreateGreenGrading(ctx, CreateGreenGradingInput{SampleBusinessID: "SMP-NOPE"}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("unknown sample: %v", err)
		}
	})
}
