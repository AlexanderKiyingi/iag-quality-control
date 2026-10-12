package pdf

import (
	"testing"
)

func TestRenderCoA(t *testing.T) {
	raw, err := RenderCoA(CoAData{
		CoaNumber: "COA-001", LotBusinessID: "LOT-EXP-1", IssuedBy: "QC", IssuedAt: "2026-06-09", Status: "active",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) < 100 {
		t.Fatalf("expected pdf bytes, got %d", len(raw))
	}
	if raw[0] != '%' {
		t.Fatalf("expected PDF header")
	}
}

func TestRenderCupping(t *testing.T) {
	raw, err := RenderCupping(CuppingData{
		SessionID: "CUP-1", SampleID: "SMP-1", BatchBusinessID: "BAT-1",
		SessionDate: "2026-06-09", TotalScore: 85, Grade: "Specialty",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) < 100 {
		t.Fatalf("expected pdf bytes")
	}
}

func TestRenderTemplatedCoA(t *testing.T) {
	raw, err := RenderCoA(CoAData{CoaNumber: "COA-1", LotBusinessID: "LOT-1", IssuedBy: "qa@iag", IssuedAt: "2026-10-13",
		Template: &CoALayout{Title: "Certificate of Analysis", Customer: "Café Buyer", FooterStatement: "Tested to buyer spec — ≤ 12.5",
			Rows: []CoARow{{Label: "Humidity", Result: "11.2", Unit: "%", Spec: "≤ 12.5", Verdict: "pass"}, {Label: "Above 18", Unit: "%", Spec: "—"}}}})
	if err != nil || len(raw) < 500 || string(raw[:4]) != "%PDF" {
		t.Fatalf("templated coa: %d bytes %v", len(raw), err)
	}
}
