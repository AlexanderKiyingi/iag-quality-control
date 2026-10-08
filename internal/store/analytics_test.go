package store

import "testing"

func TestMeanStd(t *testing.T) {
	mean, std := meanStd([]float64{10, 12, 14})
	if mean != 12 {
		t.Fatalf("mean=%v", mean)
	}
	if std <= 0 {
		t.Fatalf("std=%v", std)
	}
}

func TestSPCBadMetric(t *testing.T) {
	s := &Store{}
	// Any parameter can be charted now; only one with no name left after
	// canonicalising is refused, and refused before the database is touched.
	_, err := s.SPC(t.Context(), SPCOptions{Metric: "%%%", Days: 7})
	if err != ErrBadInput {
		t.Fatalf("expected ErrBadInput, got %v", err)
	}
}

func TestCapability(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	cp, cpk := capability(11, 0.5, f(10), f(12.5))
	if cp == nil || *cp != 0.83 || cpk == nil || *cpk != 0.67 {
		t.Fatalf("two-sided: cp %v cpk %v", cp, cpk)
	}
	cp, cpk = capability(11, 0.5, nil, f(12.5))
	if cp != nil || cpk == nil || *cpk != 1 {
		t.Fatalf("upper only: Cp undefined, Cpu = 1; got cp %v cpk %v", cp, cpk)
	}
	if cp, cpk = capability(11, 0, nil, f(12.5)); cp != nil || cpk != nil {
		t.Fatal("no spread, no index")
	}
}
