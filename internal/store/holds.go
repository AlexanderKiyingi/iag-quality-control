package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// HoldEvent is one entry in the hold and release log (migration 012).
//
// Append-only, following qc_custody_logs: a hold log that can be edited is not
// a hold log. There is no updated_at because no row is ever updated.
type HoldEvent struct {
	BusinessID   string         `json:"business_id"`
	EventDate    *string        `json:"event_date,omitempty"`
	BatchRef     string         `json:"batch_ref"`
	Event        string         `json:"event"`
	Reason       string         `json:"reason"`
	RecordedBy   string         `json:"recorded_by"`
	HoldLocation string         `json:"hold_location"`
	Status       string         `json:"status"`
	Notes        string         `json:"notes"`
	Attrs        map[string]any `json:"attrs"`
	CreatedAt    time.Time      `json:"created_at"`
}

// CreateHoldEventInput is the write shape. Append-only, so nothing is optional
// in the preserve sense — every field is sent or defaulted.
type CreateHoldEventInput struct {
	BusinessID   string
	BatchRef     string
	Event        string
	EventDate    string
	Reason       string
	RecordedBy   string
	HoldLocation string
	Status       string
	Notes        string
	Attrs        map[string]any
}

// normalizeHoldEvent maps the app's event labels onto the stored vocabulary.
func normalizeHoldEvent(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "placed on hold", "placed_on_hold", "hold", "on hold":
		return "placed_on_hold"
	case "released", "release":
		return "released"
	case "rejected", "reject":
		return "rejected"
	case "reworked", "rework":
		return "reworked"
	default:
		return strings.TrimSpace(v)
	}
}

// normalizeHoldStatus maps the app's status labels onto the stored vocabulary.
func normalizeHoldStatus(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "active hold", "active_hold", "active":
		return "active_hold"
	case "cleared":
		return "cleared"
	case "scrapped":
		return "scrapped"
	default:
		return strings.TrimSpace(v)
	}
}

const holdEventCols = `business_id, event_date::text, batch_ref, event, reason, recorded_by,
	hold_location, status, notes, attrs, created_at`

func (s *Store) ListHoldEvents(ctx context.Context, batchRef, status string, limit int) ([]HoldEvent, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+holdEventCols+`
		FROM qc_hold_events
		WHERE ($1 = '' OR batch_ref = $1) AND ($2 = '' OR status = $2)
		ORDER BY event_date DESC NULLS LAST, created_at DESC
		LIMIT $3`, strings.TrimSpace(batchRef), normalizeHoldStatus(status), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []HoldEvent{}
	for rows.Next() {
		item, err := scanHoldEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// CreateHoldEvent appends one event.
//
// A caller-supplied business_id that already exists is a conflict, not an
// update: re-posting the same reference must not rewrite history. That surfaces
// as 409, which is a visible behaviour difference from the register resources
// the rest of this app upserts — and the right one for an audit log.
func (s *Store) CreateHoldEvent(ctx context.Context, in CreateHoldEventInput) (HoldEvent, error) {
	batchRef := strings.TrimSpace(in.BatchRef)
	event := normalizeHoldEvent(in.Event)
	if batchRef == "" || event == "" {
		return HoldEvent{}, ErrBadInput
	}
	id := strings.TrimSpace(in.BusinessID)
	if id == "" {
		var err error
		if id, err = nextBusinessID(ctx, s.pool, "qc_hold_events", "HLD"); err != nil {
			return HoldEvent{}, err
		}
	}
	status := normalizeHoldStatus(in.Status)
	if status == "" {
		status = "active_hold"
	}
	rows, err := s.pool.Query(ctx, `
		INSERT INTO qc_hold_events (
			business_id, event_date, batch_ref, event, reason, recorded_by,
			hold_location, status, notes, attrs
		) VALUES (
			$1, COALESCE(NULLIF($2::text, '')::date, CURRENT_DATE), $3, $4, $5, $6, $7, $8, $9,
			COALESCE($10::jsonb, '{}'::jsonb)
		)
		RETURNING `+holdEventCols,
		id, strings.TrimSpace(in.EventDate), batchRef, event, in.Reason,
		strings.TrimSpace(in.RecordedBy), strings.TrimSpace(in.HoldLocation), status,
		in.Notes, attrsOptional(in.Attrs),
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return HoldEvent{}, ErrConflict
		}
		return HoldEvent{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				return HoldEvent{}, ErrConflict
			}
			return HoldEvent{}, err
		}
		return HoldEvent{}, ErrNotFound
	}
	return scanHoldEvent(rows)
}

func scanHoldEvent(rows interface{ Scan(...any) error }) (HoldEvent, error) {
	var item HoldEvent
	var attrs []byte
	if err := rows.Scan(&item.BusinessID, &item.EventDate, &item.BatchRef, &item.Event,
		&item.Reason, &item.RecordedBy, &item.HoldLocation, &item.Status, &item.Notes,
		&attrs, &item.CreatedAt); err != nil {
		return HoldEvent{}, err
	}
	item.Attrs = attrsMap(attrs)
	return item, nil
}
