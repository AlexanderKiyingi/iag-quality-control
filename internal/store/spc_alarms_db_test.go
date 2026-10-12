package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"iag-quality-control/backend/internal/db"
	"iag-quality-control/backend/internal/migrate"
)

// Control charts, alarms and the quality dashboard (022) against a real
// Postgres. Env-gated; see TestUpsertGuardsAgainstRealPostgres.
func TestSPCChartsAndAlarmsAgainstRealPostgres(t *testing.T) {
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
	run := strings.ReplaceAll(time.Now().UTC().Format("150405.000"), ".", "")
	param := "viscosity_" + run
	var samples []string
	for i := 0; i < 3; i++ {
		smp, err := s.CreateSample(ctx, CreateSampleInput{BatchBusinessID: fmt.Sprintf("BAT-SPC%d-%s", i, run),
			SampleID: fmt.Sprintf("SMP-SPC%d-%s", i, run)})
		if err != nil {
			t.Fatal(err)
		}
		samples = append(samples, smp.BusinessID)
	}
	record := func(sample string, v float64) LabMeasurement {
		t.Helper()
		m, err := s.CreateLabMeasurement(ctx, CreateLabMeasurementInput{SampleBusinessID: sample, Parameter: param,
			Value: fmt.Sprintf("%g", v), Actor: "ana@iag"})
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	stable := []float64{10.1, 9.9, 10.0, 10.2, 9.8, 10.1, 9.9, 10.0, 10.1}
	for i, v := range stable {
		m := record(samples[i%3], v)
		if alerts, err := s.CheckSPCAlarms(ctx, m.BusinessID, []string{param}, "ana@iag"); err != nil || len(alerts) != 0 {
			t.Fatalf("a stable reading alarmed: %+v %v", alerts, err)
		}
	}

	t.Run("an outlier breaks rule 1 once, and a raise_nc parameter raises an NC", func(t *testing.T) {
		if _, err := s.PutSPCSettings(ctx, SPCSettings{Parameter: param, Rules: []int{1, 2, 3, 5, 6}, Alarms: true, RaiseNC: true}, "ana@iag"); err != nil {
			t.Fatal(err)
		}
		m := record(samples[0], 14.0)
		alerts, err := s.CheckSPCAlarms(ctx, m.BusinessID, []string{param}, "ana@iag")
		if err != nil || len(alerts) == 0 || alerts[0].Rule != 1 || alerts[0].NonConformance == "" {
			t.Fatalf("outlier: %+v %v", alerts, err)
		}
		again, _ := s.CheckSPCAlarms(ctx, m.BusinessID, []string{param}, "ana@iag")
		if len(again) != 0 {
			t.Fatalf("a reading alarms once: %+v", again)
		}
		nc, err := s.GetNonConformance(ctx, alerts[0].NonConformance)
		if err != nil || !strings.HasPrefix(nc.Title, "Out of statistical control") {
			t.Fatalf("nc: %+v %v", nc, err)
		}
		open, _ := s.ListSPCAlerts(ctx, "open", param, 10)
		if len(open) != len(alerts) {
			t.Fatalf("open alerts: %+v", open)
		}
		if err := s.AcknowledgeSPCAlert(ctx, open[0].ID, "ana@iag", ""); !errors.Is(err, ErrReasonRequired) {
			t.Fatalf("ack without a note: %v", err)
		}
		if err := s.AcknowledgeSPCAlert(ctx, open[0].ID, "ana@iag", "viscometer not zeroed"); err != nil {
			t.Fatal(err)
		}
		if err := s.AcknowledgeSPCAlert(ctx, open[0].ID, "ana@iag", "again"); !errors.Is(err, ErrConflict) {
			t.Fatalf("ack twice: %v", err)
		}
	})

	t.Run("settings changes need a reason once set, and bad rules are refused", func(t *testing.T) {
		if _, err := s.PutSPCSettings(ctx, SPCSettings{Parameter: param, Rules: []int{1}}, "ana@iag"); !errors.Is(err, ErrReasonRequired) {
			t.Fatalf("no reason: %v", err)
		}
		if _, err := s.PutSPCSettings(ctx, SPCSettings{Parameter: param, Rules: []int{4}, ChangeReason: "x"}, "ana@iag"); !errors.Is(err, ErrBadInput) {
			t.Fatalf("rule 4: %v", err)
		}
		got, err := s.PutSPCSettings(ctx, SPCSettings{Parameter: param, Rules: []int{1}, Alarms: false, ChangeReason: "quiet while recalibrating"}, "ana@iag")
		if err != nil || got.Alarms || got.Default {
			t.Fatalf("settings: %+v %v", got, err)
		}
		m := record(samples[1], 20)
		if alerts, _ := s.CheckSPCAlarms(ctx, m.BusinessID, []string{param}, "ana@iag"); len(alerts) != 0 {
			t.Fatalf("alarms off still alarmed: %+v", alerts)
		}
	})

	t.Run("I-MR limits come from the moving range; X̄-R groups by sample", func(t *testing.T) {
		imr, err := s.SPC(ctx, SPCOptions{Metric: param, Days: 7})
		if err != nil || imr.Chart != "i_mr" || imr.MRLimits == nil || len(imr.MovingRanges) != imr.Count-1 || imr.RuleBreaks == 0 {
			t.Fatalf("i_mr: %+v %v", imr, err)
		}
		xr, err := s.SPC(ctx, SPCOptions{Metric: param, Days: 7, Chart: "xbar_r", SubgroupBy: "sample"})
		if err != nil || len(xr.Subgroups) != 3 || xr.SubgroupBy != "sample" || xr.Subgroups[0].N < 2 {
			t.Fatalf("xbar_r: %+v %v", xr, err)
		}
		if _, err := s.SPC(ctx, SPCOptions{Metric: param, Days: 7, Chart: "p"}); !errors.Is(err, ErrBadInput) {
			t.Fatalf("p chart without spec: %v", err)
		}
		usl := 12.0
		p, err := s.SPC(ctx, SPCOptions{Metric: param, Days: 7, Chart: "p", USL: &usl})
		if err != nil || len(p.Proportions) == 0 || p.Proportions[0].Defective != 2 {
			t.Fatalf("p chart: %+v %v", p.Proportions, err)
		}
		if _, err := s.SPC(ctx, SPCOptions{Metric: param, Chart: "pie"}); !errors.Is(err, ErrBadInput) {
			t.Fatalf("unknown chart: %v", err)
		}
		fr, err := s.SPC(ctx, SPCOptions{Metric: "failure_rate", Days: 7})
		if err != nil || fr.Chart != "p" {
			t.Fatalf("failure rate: %+v %v", fr, err)
		}
	})

	t.Run("the dashboard covers the window and its open work", func(t *testing.T) {
		d, err := s.QualityDashboard(ctx, 14)
		if err != nil {
			t.Fatal(err)
		}
		today := d.Daily[len(d.Daily)-1]
		if len(d.Daily) != 14 || today.SamplesReceived < 3 || today.Results < 11 {
			t.Fatalf("daily: %d days, today %+v", len(d.Daily), today)
		}
		if d.NCsRaised < 1 {
			t.Fatalf("ncs raised: %+v", d)
		}
	})
}
