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
	_, err := s.SPC(t.Context(), SPCOptions{Metric: "invalid", Days: 7})
	if err != ErrBadInput {
		t.Fatalf("expected ErrBadInput, got %v", err)
	}
}
