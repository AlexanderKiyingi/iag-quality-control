package pdf

import (
	"bytes"
	"testing"
	"time"
)

func TestRenderAuditTrail(t *testing.T) {
	entries := make([]AuditEntry, 0, 120)
	for i := 0; i < 120; i++ {
		entries = append(entries, AuditEntry{When: time.Now(), Entity: "samples", Record: "SMP-26-0001", Op: "UPDATE",
			Actor: "ana@iag", Reason: "relabelled — café origin",
			Changes: map[string]any{"status": map[string]any{"old": "pending", "new": "in-progress"}, "notes": map[string]any{"old": nil, "new": "Ñ"}}})
	}
	raw, err := RenderAuditTrail(AuditTrailData{Filters: []string{"entity = samples"}, GeneratedAt: time.Now(), Entries: entries, Truncated: true})
	if err != nil || !bytes.HasPrefix(raw, []byte("%PDF")) || len(raw) < 5000 {
		t.Fatalf("pdf: %d bytes, %v", len(raw), err)
	}
	empty, err := RenderAuditTrail(AuditTrailData{GeneratedAt: time.Now()})
	if err != nil || !bytes.HasPrefix(empty, []byte("%PDF")) {
		t.Fatalf("empty: %v", err)
	}
}
