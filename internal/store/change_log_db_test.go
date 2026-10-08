package store

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"iag-quality-control/backend/internal/db"
	"iag-quality-control/backend/internal/migrate"
)

// The change log (017) against a real Postgres. Env-gated; see
// TestUpsertGuardsAgainstRealPostgres for how to run it.
func TestChangeLogAgainstRealPostgres(t *testing.T) {
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
	str := func(v string) *string { return &v }
	num := func(v float64) *float64 { return &v }
	id := fmt.Sprintf("IIN-LOG-%s", run)

	if _, err := s.UpsertIncomingInspection(ctx, UpsertIncomingInspectionInput{
		BusinessID: id, SourceLot: "LOT-LOG-" + run, MoisturePct: num(11.0), Actor: "ana@iag",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertIncomingInspection(ctx, UpsertIncomingInspectionInput{
		BusinessID: id, SourceLot: "LOT-LOG-" + run, MoisturePct: num(11.4), Actor: "ben@iag",
	}); err != nil {
		t.Fatal(err)
	}
	// A re-save that changes nothing logs nothing.
	if _, err := s.UpsertIncomingInspection(ctx, UpsertIncomingInspectionInput{
		BusinessID: id, SourceLot: "LOT-LOG-" + run, Notes: str(""), Actor: "ben@iag",
	}); err != nil {
		t.Fatal(err)
	}

	entries, err := s.ListChangeLog(ctx, "incoming-inspections", id, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("want an INSERT and one UPDATE, got %d: %+v", len(entries), entries)
	}
	upd, ins := entries[0], entries[1]
	if ins.Op != "INSERT" || ins.Actor != "ana@iag" || upd.Op != "UPDATE" || upd.Actor != "ben@iag" {
		t.Fatalf("ops/actors: %+v / %+v", ins, upd)
	}
	moisture, ok := upd.Changes["moisture_pct"].(map[string]any)
	if !ok || moisture["old"] != 11.0 || moisture["new"] != 11.4 {
		t.Fatalf("moisture before/after: %#v", upd.Changes)
	}
	if _, ok := upd.Changes["updated_at"]; ok {
		t.Fatal("bookkeeping columns must not appear in a diff")
	}

	if _, err := pool.Exec(ctx, `UPDATE qc_change_log SET actor = 'mallory' WHERE id = $1`, upd.ID); err == nil ||
		!strings.Contains(err.Error(), "append-only") {
		t.Fatalf("editing the log must be refused, got %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM qc_change_log WHERE id = $1`, upd.ID); err == nil {
		t.Fatal("deleting from the log must be refused")
	}
	if _, err := s.ListChangeLog(ctx, "pg_authid", "", "", 1); err == nil {
		t.Fatal("an unknown entity must be refused, not queried")
	}
}
