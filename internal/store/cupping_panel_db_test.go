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

// Per-evaluator cupping sheets (018) against a real Postgres. Env-gated; see
// TestUpsertGuardsAgainstRealPostgres for how to run it.
func TestCuppingPanelAgainstRealPostgres(t *testing.T) {
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
	sample, err := s.CreateSample(ctx, CreateSampleInput{BatchBusinessID: "BAT-CUP-" + run, SampleID: "SMP-CUP-" + run})
	if err != nil {
		t.Fatal(err)
	}
	sheet := func(who string, each float64) CuppingScoreInput {
		return CuppingScoreInput{Evaluator: who, Fragrance: each, Flavor: each, Aftertaste: each,
			Acidity: each, Body: each, Balance: each, Uniformity: each, CleanCup: each,
			Sweetness: each, Overall: each}
	}

	session, err := s.CreateCupping(ctx, CreateCuppingInput{
		SampleBusinessID: sample.BusinessID,
		// Ignored when sheets are sent.
		Fragrance: 1,
		Scores:    []CuppingScoreInput{sheet("Ana", 8.25), sheet("Ben", 8.3), sheet("Cy", 8.2), sheet("Dee", 7.6)},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Mean of 82.5, 83, 82, 76.
	if session.TotalScore != 80.88 || session.Fragrance != 8.09 || len(session.Scorers) != 4 {
		t.Fatalf("session should hold the panel mean: total %v fragrance %v scorers %v",
			session.TotalScore, session.Fragrance, session.Scorers)
	}
	panel, err := s.GetCuppingPanel(ctx, session.BusinessID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(panel.Scores) != 4 || panel.Stats.OutlierCount != 1 || panel.Stats.Evaluators[0].Evaluator != "Dee" {
		t.Fatalf("panel: %+v", panel.Stats)
	}

	if _, err := s.CreateCupping(ctx, CreateCuppingInput{
		SampleBusinessID: sample.BusinessID,
		Scores:           []CuppingScoreInput{sheet("Ana", 8), sheet("ana ", 8)},
	}); !errors.Is(err, ErrBadInput) {
		t.Fatalf("one evaluator, two sheets: want ErrBadInput, got %v", err)
	}

	legacy, err := s.CreateCupping(ctx, CreateCuppingInput{SampleBusinessID: sample.BusinessID, Fragrance: 8})
	if err != nil {
		t.Fatal(err)
	}
	empty, err := s.GetCuppingPanel(ctx, legacy.BusinessID, 0)
	if err != nil || len(empty.Scores) != 0 || empty.Stats.PanelSize != 0 {
		t.Fatalf("a session without sheets has an empty panel: %+v %v", empty, err)
	}
	// Sheets saved one at a time, as each cupper fills in their own: the
	// legacy session's typed-in scores give way to its panel's mean.
	for _, sh := range []CuppingScoreInput{sheet("Ana", 8.0), sheet("Ben", 8.5)} {
		if _, err := s.SaveCuppingScore(ctx, legacy.BusinessID, sh); err != nil {
			t.Fatal(err)
		}
	}
	// Ben corrects his sheet.
	if _, err := s.SaveCuppingScore(ctx, legacy.BusinessID, sheet("Ben", 8.2)); err != nil {
		t.Fatal(err)
	}
	after, err := s.GetLatestCuppingBySample(ctx, sample.BusinessID)
	if err != nil || after.BusinessID != legacy.BusinessID || after.TotalScore != 81 || len(after.Scorers) != 2 {
		t.Fatalf("session should be the mean of 80 and 82: %+v %v", after, err)
	}
	listed, err := s.ListCuppingScores(ctx, legacy.BusinessID, 0)
	if err != nil || len(listed) != 2 || listed[0].PanelSize != 2 || listed[0].Deviation == 0 {
		t.Fatalf("listed sheets: %+v %v", listed, err)
	}
	if _, err := s.SaveCuppingScore(ctx, "CUP-NOPE-"+run, sheet("Ana", 8)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("sheet on an unknown session: want ErrNotFound, got %v", err)
	}
	if _, err := s.GetCuppingPanel(ctx, "CUP-NOPE-"+run, 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown session: want ErrNotFound, got %v", err)
	}
	entries, err := s.ListChangeLog(ctx, "", session.BusinessID, "", 50)
	if err != nil || len(entries) < 5 {
		t.Fatalf("session + four sheets should be in the change log under the session id: %d %v", len(entries), err)
	}
}
