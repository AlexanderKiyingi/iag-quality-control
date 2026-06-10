package labels

import "testing"

func TestRenderZPLContainsSampleID(t *testing.T) {
	l := NewSampleLabel("SMP-0001", "BAT-9", "Green - Incoming")
	zpl := RenderZPL(l)
	if zpl == "" || !contains(zpl, "SMP-0001") {
		t.Fatalf("zpl missing sample id: %q", zpl)
	}
}

func TestRenderSVG(t *testing.T) {
	l := NewSampleLabel("SMP-0001", "BAT-9", "Green")
	svg := RenderSVG(l)
	if svg == "" || !contains(svg, "SMP-0001") {
		t.Fatalf("svg missing sample id")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
