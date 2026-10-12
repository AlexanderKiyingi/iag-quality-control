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

// Cupping tables, blind codes and the CVA (020) against a real Postgres.
// Env-gated; see TestUpsertGuardsAgainstRealPostgres.
func TestCuppingTablesAgainstRealPostgres(t *testing.T) {
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
	var samples []string
	for _, n := range []string{"A", "B", "C"} {
		smp, err := s.CreateSample(ctx, CreateSampleInput{BatchBusinessID: "BAT-CT" + n + "-" + run, SampleID: "SMP-CT" + n + "-" + run})
		if err != nil {
			t.Fatal(err)
		}
		samples = append(samples, smp.BusinessID)
	}
	allHedonic := func(v int) map[string]int {
		m := map[string]int{}
		for _, k := range []string{"fragrance", "aroma", "flavor", "aftertaste", "acidity", "sweetness", "mouthfeel", "overall"} {
			m[k] = v
		}
		return m
	}

	table, err := s.CreateCuppingTable(ctx, CreateCuppingTableInput{Title: "Morning " + run, Form: "sca_cva",
		SampleIDs: samples, Actor: "ana@iag"})
	if err != nil {
		t.Fatal(err)
	}

	t.Run("a blind table hides the lots behind distinct codes", func(t *testing.T) {
		if !table.Blind || table.Revealed || len(table.Cups) != 3 || table.CuppingTable.Cups != 3 {
			t.Fatalf("table: %+v", table)
		}
		seen := map[string]bool{}
		sessions := map[string]bool{}
		for _, cup := range table.Cups {
			if len(cup.CupCode) != 3 || seen[cup.CupCode] {
				t.Fatalf("codes must be three digits and distinct: %+v", table.Cups)
			}
			seen[cup.CupCode] = true
			sessions[cup.SessionBusinessID] = true
			if cup.SampleBusinessID != "" || cup.BatchBusinessID != "" {
				t.Fatalf("an open blind table must not name the lot: %+v", cup)
			}
		}
		if len(sessions) != 3 {
			t.Fatalf("each sample needs its own session id: %+v", table.Cups)
		}
		revealed, err := s.GetCuppingTable(ctx, table.BusinessID, true)
		if err != nil || revealed.Cups[0].SampleBusinessID == "" {
			t.Fatalf("reveal: %+v %v", revealed, err)
		}
	})

	cup := table.Cups[0]
	session, err := s.SessionForCup(ctx, table.BusinessID, cup.CupCode)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("a CVA sheet is scored by the standard formula", func(t *testing.T) {
		sheet, err := s.SaveCuppingScore(ctx, session, CuppingScoreInput{Evaluator: "Ana", Form: "sca_cva",
			Hedonic: allHedonic(9), Descriptors: []string{"jasmine", "Jasmine ", "bergamot"}, ClientRef: "tab1-" + run})
		if err != nil {
			t.Fatal(err)
		}
		if sheet.TotalScore != 100 {
			t.Fatalf("all-9 is 100, got %v", sheet.TotalScore)
		}
		sheet, err = s.SaveCuppingScore(ctx, session, CuppingScoreInput{Evaluator: "Ben", Form: "sca_cva",
			Hedonic: allHedonic(5), NonUniformCups: 1})
		if err != nil {
			t.Fatal(err)
		}
		if sheet.TotalScore != 77 {
			t.Fatalf("all-5 with one non-uniform cup is 79 − 2 = 77, got %v", sheet.TotalScore)
		}
		if _, err := s.SaveCuppingScore(ctx, session, CuppingScoreInput{Evaluator: "Cy", Form: "sca_cva",
			Hedonic: map[string]int{"overall": 7}}); !errors.Is(err, ErrBadInput) {
			t.Fatalf("a CVA sheet missing sections: want ErrBadInput, got %v", err)
		}
	})

	t.Run("a tablet re-sending the same sheet records it once", func(t *testing.T) {
		// The same sheet again is a no-op update, not an error: the tablet
		// does not need to tell "synced earlier" from "synced now".
		if _, err := s.SaveCuppingScore(ctx, session, CuppingScoreInput{Evaluator: "Ana", Form: "sca_cva",
			Hedonic: allHedonic(9), ClientRef: "tab1-" + run}); err != nil {
			t.Fatalf("same sheet re-sent: %v", err)
		}
		// The same ref on another cupper's sheet is a client bug, refused.
		if _, err := s.SaveCuppingScore(ctx, session, CuppingScoreInput{Evaluator: "Cy", Form: "sca_cva",
			Hedonic: allHedonic(6), ClientRef: "tab1-" + run}); !errors.Is(err, ErrConflict) {
			t.Fatalf("ref reused by another sheet: want ErrConflict, got %v", err)
		}
		panel, err := s.GetCuppingPanel(ctx, session, 0)
		if err != nil || len(panel.Scores) != 2 {
			t.Fatalf("still two sheets: %+v %v", panel.Scores, err)
		}
	})

	t.Run("open blind cups show no running score; closing reveals and freezes", func(t *testing.T) {
		open, err := s.GetCuppingTable(ctx, table.BusinessID, false)
		if err != nil || open.Cups[0].MeanScore != nil || len(open.Cups[0].Evaluators) != 2 {
			t.Fatalf("open: %+v %v", open.Cups[0], err)
		}
		closed, err := s.CloseCuppingTable(ctx, table.BusinessID, "ana@iag", "")
		if err != nil || !closed.Revealed || closed.Status != "closed" || closed.Cups[0].MeanScore == nil {
			t.Fatalf("closed: %+v %v", closed, err)
		}
		if _, err := s.SaveCuppingScore(ctx, session, CuppingScoreInput{Evaluator: "Dee", Form: "sca_cva",
			Hedonic: allHedonic(7)}); !errors.Is(err, ErrTableClosed) {
			t.Fatalf("a closed table refuses sheets: got %v", err)
		}
		if log, err := s.ListChangeLog(ctx, "cupping-tables", table.BusinessID, "", 5); err != nil || len(log) < 2 {
			t.Fatalf("closing a table belongs in the change log: %+v %v", log, err)
		}
		if _, err := s.CloseCuppingTable(ctx, table.BusinessID, "ana@iag", ""); !errors.Is(err, ErrTableClosed) {
			t.Fatalf("closing twice: got %v", err)
		}
		if _, err := s.CloseCuppingTable(ctx, "CT-NOPE", "ana@iag", ""); !errors.Is(err, ErrNotFound) {
			t.Fatalf("closing a missing table: got %v", err)
		}
	})

	t.Run("a calibration table measures cuppers against the reference", func(t *testing.T) {
		if _, err := s.CreateCuppingTable(ctx, CreateCuppingTableInput{Calibration: true, SampleIDs: samples[:1]}); !errors.Is(err, ErrBadInput) {
			t.Fatalf("calibration without a reference: got %v", err)
		}
		cal, err := s.CreateCuppingTable(ctx, CreateCuppingTableInput{Form: "sca_cva", Calibration: true,
			ReferenceEvaluator: "Head", SampleIDs: samples[1:2]})
		if err != nil {
			t.Fatal(err)
		}
		sess, _ := s.SessionForCup(ctx, cal.BusinessID, cal.Cups[0].CupCode)
		for who, v := range map[string]int{"Head": 7, "Ana": 7, "Ben": 4} {
			if _, err := s.SaveCuppingScore(ctx, sess, CuppingScoreInput{Evaluator: who, Form: "sca_cva", Hedonic: allHedonic(v)}); err != nil {
				t.Fatal(err)
			}
		}
		panel, err := s.GetCuppingPanel(ctx, sess, 0)
		if err != nil {
			t.Fatal(err)
		}
		if panel.Stats.Reference != "Head" || panel.Stats.OutlierCount != 1 || panel.Stats.Evaluators[0].Evaluator != "Ben" {
			t.Fatalf("Ben is the one off the reference: %+v", panel.Stats)
		}
	})

	t.Run("an unknown sample fails the whole table", func(t *testing.T) {
		before, _ := s.ListCuppingTables(ctx, "", 500)
		if _, err := s.CreateCuppingTable(ctx, CreateCuppingTableInput{SampleIDs: []string{samples[0], "SMP-NOPE"}}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("got %v", err)
		}
		after, _ := s.ListCuppingTables(ctx, "", 500)
		if len(after) != len(before) {
			t.Fatal("a table with a bad sample must not be half-created")
		}
	})
}
