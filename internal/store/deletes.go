package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

/*
Deleting a register row — and the far longer list of things that must not be.

The Lab app offered Delete on every screen and every one answered 405, because
this service had no DELETE anywhere. The fix is not a DELETE on everything: most
of what this service holds is a quality record, and a LIMS whose results can be
removed is not evidence of anything.

So deletion is allowed only where a row is **planning data that has not been
acted on yet** — a method still in draft, a request nobody has started, a study
not begun, a check still open. Once a row has been approved, reported, closed or
used, it stays, and the caller gets a 409 saying so. A mistake after that point
is corrected by a status change (every one of these registers has a Void or
Cancelled state) which leaves the history intact.

Never deletable, and not listed here: samples, physical/chemical tests, cupping
sessions, measurements, calibrations, CoAs, certification requests, custody
logs, hold events, release decisions, CAPAs, non-conformances, compliance logs
and external audits. Those are the audit trail.
*/

// deletableWhileStatus names the register tables a row may be removed from, and
// the statuses a row must be in to qualify.
var deletableWhileStatus = map[string][]string{
	"qc_lab_methods":          {"draft"},
	"qc_lab_requests":         {"draft", "open"},
	"qc_stability_studies":    {"planned"},
	"qc_in_process_checks":    {"open"},
	"qc_incoming_inspections": {"open"},
}

// DeleteRegisterRow removes one row when its register allows it and the row has
// not been acted on.
//
// Returns ErrNotFound when the row is gone, and ErrConflict when the register
// allows deletion in principle but this row's status has moved past it — the
// caller should void it instead.
func (s *Store) DeleteRegisterRow(ctx context.Context, table, businessID string) error {
	allowed, ok := deletableWhileStatus[table]
	if !ok {
		return ErrBadInput
	}
	id := strings.TrimSpace(businessID)
	if id == "" {
		return ErrBadInput
	}

	var status string
	// #nosec G201 -- table comes from the map above, never from a request.
	err := s.pool.QueryRow(ctx,
		fmt.Sprintf(`SELECT status FROM %s WHERE business_id = $1`, table), id).Scan(&status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if !containsFold(allowed, status) {
		return ErrConflict
	}

	tag, err := s.pool.Exec(ctx,
		fmt.Sprintf(`DELETE FROM %s WHERE business_id = $1 AND status = ANY($2)`, table),
		id, allowed)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		// Somebody moved it on between the read and the delete.
		return ErrConflict
	}
	return nil
}

func containsFold(list []string, value string) bool {
	v := strings.ToLower(strings.TrimSpace(value))
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}
