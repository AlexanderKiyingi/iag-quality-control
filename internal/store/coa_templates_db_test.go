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

// CoA templates and alert routes (026) against a real Postgres. Env-gated;
// see TestUpsertGuardsAgainstRealPostgres.
func TestCoATemplatesAndAlertRoutesAgainstRealPostgres(t *testing.T) {
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
	customer := "CUST-" + run

	t.Run("a customer's template is found for its certificates", func(t *testing.T) {
		if _, err := s.PutCoATemplate(ctx, CoATemplateInput{Name: "empty", Parameters: []CoAField{{Parameter: "  "}}}); !errors.Is(err, ErrBadInput) {
			t.Fatalf("a template with nothing to print: %v", err)
		}
		tpl, err := s.PutCoATemplate(ctx, CoATemplateInput{Name: "Buyer layout", CustomerRef: customer,
			Parameters: []CoAField{{Parameter: "Moisture %", Label: "Humidity", Unit: "%", ShowSpec: true}, {Parameter: "screen_18_plus_pct", Label: "Above 18"}},
			FooterStatement: "Tested to buyer spec.", Actor: "qa@iag"})
		if err != nil || tpl.Parameters[0].Parameter != "moisture" || tpl.Parameters[0].Label != "Humidity" {
			t.Fatalf("put: %+v %v", tpl, err)
		}
		got, ok, err := s.ResolveCoATemplate(ctx, "", customer)
		if err != nil || !ok || got.BusinessID != tpl.BusinessID {
			t.Fatalf("resolve by customer: %+v %v %v", got, ok, err)
		}
		if _, err := s.PutCoATemplate(ctx, CoATemplateInput{Name: "second", CustomerRef: customer,
			Parameters: []CoAField{{Parameter: "moisture"}}}); !errors.Is(err, ErrConflict) {
			t.Fatalf("two active templates for one customer: %v", err)
		}
		if _, err := s.PutCoATemplate(ctx, CoATemplateInput{BusinessID: tpl.BusinessID, Name: "Buyer layout v2",
			CustomerRef: customer, Parameters: []CoAField{{Parameter: "moisture"}}}); !errors.Is(err, ErrReasonRequired) {
			t.Fatalf("an edit without a reason: %v", err)
		}
		// A certificate issued for the customer carries it, and finds the template.
		coa, err := s.CreateCoA(ctx, CreateCoAInput{CoaNumber: "COA-CTPL-" + run, LotBusinessID: "LOT-CTPL-" + run,
			IssuedBy: "qa@iag", CustomerRef: customer})
		if err != nil || coa.CustomerRef != customer {
			t.Fatalf("issue: %+v %v", coa, err)
		}
		again, err := s.GetCoAByNumber(ctx, coa.CoaNumber)
		if err != nil || again.CustomerRef != customer {
			t.Fatalf("read back: %+v %v", again, err)
		}
		if found, ok, _ := s.ResolveCoATemplate(ctx, again.TemplateRef, again.CustomerRef); !ok || found.BusinessID != tpl.BusinessID {
			t.Fatalf("the issued CoA resolves its customer's template: %+v", found)
		}
		if list, err := s.ListCoA(ctx, coa.LotBusinessID, 10); err != nil || len(list) != 1 || list[0].CustomerRef != customer {
			t.Fatalf("list: %+v %v", list, err)
		}
		if _, ok, _ := s.ResolveCoATemplate(ctx, "", "NOBODY-"+run); ok {
			// Only when no house default exists in this database; a default is fine too.
			def, _, _ := s.ResolveCoATemplate(ctx, "", "NOBODY-"+run)
			if !def.IsDefault {
				t.Fatalf("an unknown customer gets the default or nothing: %+v", def)
			}
		}
	})

	t.Run("a batch's latest results are gathered across registers", func(t *testing.T) {
		sample, err := s.CreateSample(ctx, CreateSampleInput{BatchBusinessID: "BAT-CTPL-" + run, SampleID: "SMP-CTPL-" + run})
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range []string{"11.9", "11.2"} {
			if _, err := s.CreateLabMeasurement(ctx, CreateLabMeasurementInput{SampleBusinessID: sample.BusinessID, Parameter: "Moisture %", Value: v, Actor: "ana@iag"}); err != nil {
				t.Fatal(err)
			}
		}
		res, err := s.BatchResults(ctx, sample.BatchBusinessID)
		if err != nil || res["moisture"].Value != 11.2 {
			t.Fatalf("latest wins: %+v %v", res, err)
		}
	})

	t.Run("alert routes are seeded and changed with a reason", func(t *testing.T) {
		routes, err := s.ListAlertRoutes(ctx)
		if err != nil || len(routes) < 6 {
			t.Fatalf("seeded: %+v %v", routes, err)
		}
		if _, err := s.UpdateAlertRoute(ctx, AlertRoute{Kind: "spc", Audience: "quality.lab", Enabled: true}, "qa@iag"); !errors.Is(err, ErrReasonRequired) {
			t.Fatalf("no reason: %v", err)
		}
		r, err := s.UpdateAlertRoute(ctx, AlertRoute{Kind: "spc", Audience: "quality.lab", Enabled: true, ChangeReason: "lab leads own SPC"}, "qa@iag")
		if err != nil || r.Audience != "quality.lab" {
			t.Fatalf("update: %+v %v", r, err)
		}
		if _, err := s.UpdateAlertRoute(ctx, AlertRoute{Kind: "nope", Audience: "x", ChangeReason: "x"}, "qa@iag"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("unknown kind: %v", err)
		}
		_, _ = s.UpdateAlertRoute(ctx, AlertRoute{Kind: "spc", Audience: "approvals.quality", Enabled: true, ChangeReason: "test reset"}, "qa@iag")
	})
}
