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

// SPC over parameterised measurements, against a real Postgres. Env-gated; see
// TestUpsertGuardsAgainstRealPostgres for how to run it.
func TestSPCOverMeasurementsAgainstRealPostgres(t *testing.T) {
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
	batch := "BAT-SPC-" + run
	param := "chlorogenic_" + run[len(run)-3:]
	sample, err := s.CreateSample(ctx, CreateSampleInput{BatchBusinessID: batch, SampleID: "SMP-SPC-" + run})
	if err != nil {
		t.Fatal(err)
	}
	// Recorded under three spellings; all one parameter.
	for i, v := range []string{"5.1", "5.3", "4.9", "5.0", "6.4"} {
		name := []string{param, param + " %", " " + param}[i%3]
		if _, err := s.CreateLabMeasurement(ctx, CreateLabMeasurementInput{
			BusinessID: fmt.Sprintf("MSR-SPC-%s-%d", run, i), SampleBusinessID: sample.BusinessID,
			Parameter: name, Value: v,
		}); err != nil {
			t.Fatal(err)
		}
	}
	usl := 6.0
	series, err := s.SPC(ctx, SPCOptions{Metric: param, BatchID: batch, Days: 7, USL: &usl})
	if err != nil {
		t.Fatal(err)
	}
	if series.Count != 5 || series.OutOfSpecCount != 1 || series.Cp != nil || series.Cpk == nil {
		t.Fatalf("series: count %d oos %d cp %v cpk %v", series.Count, series.OutOfSpecCount, series.Cp, series.Cpk)
	}
	params, err := s.SPCParameters(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range params {
		if p.Parameter == param && p.Count == 5 {
			found = true
		}
	}
	if !found {
		t.Fatalf("%s with 5 points should be listed: %+v", param, params)
	}
}
