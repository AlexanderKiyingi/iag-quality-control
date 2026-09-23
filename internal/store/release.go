package store

import (
	"context"
	"strings"
	"time"
)

// ReleaseDecision is a judgement on whether a batch may ship (migration 012).
//
// It reuses the shape of qc_certification_approvals but not the table: that one
// is a child of the five-stage export-certification ladder and terminates in CoA
// issuance, whereas this is a standalone call on a batch.
type ReleaseDecision struct {
	BusinessID   string         `json:"business_id"`
	DecisionDate *string        `json:"decision_date,omitempty"`
	BatchRef     string         `json:"batch_ref"`
	Product      string         `json:"product"`
	CheckRefs    string         `json:"check_refs"`
	DecidedBy    string         `json:"decided_by"`
	Decision     string         `json:"decision"`
	Status       string         `json:"status"`
	Notes        string         `json:"notes"`
	Attachments  string         `json:"attachments"`
	Attrs        map[string]any `json:"attrs"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

// UpsertReleaseDecisionInput is the write shape. BatchRef is required — a release
// decision that names no batch releases nothing.
type UpsertReleaseDecisionInput struct {
	BusinessID   string
	BatchRef     string
	DecisionDate *string
	Product      *string
	CheckRefs    *string
	DecidedBy    *string
	Decision     *string
	Status       *string
	Notes        *string
	Attachments  *string
	Attrs        map[string]any
}

// NormalizeReleaseDecision maps the app's decision labels onto the stored
// vocabulary. An unrecognised word is kept rather than guessed at, and then maps
// to no event — better a missing event than a batch wrongly announced as released.
func NormalizeReleaseDecision(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "released", "release":
		return "released"
	case "conditional release", "conditional", "conditional_release":
		return "conditional"
	case "hold", "held", "on hold":
		return "hold"
	case "reject", "rejected":
		return "reject"
	case "rework":
		return "rework"
	default:
		return strings.TrimSpace(v)
	}
}

// ReleaseEventType is the domain event a decision announces, or "" for none.
//
// This is the most consequential pure function in the service: other services
// act on these events. iag-traceability gates QR publish on qc.* events and
// iag-warehouse consumes them, so a decision mapped to the wrong type moves real
// stock. An unknown decision maps to "" — silence rather than a guess.
func ReleaseEventType(decision string) string {
	switch NormalizeReleaseDecision(decision) {
	case "released", "conditional":
		return "qc.batch.released"
	case "hold", "reject", "rework":
		return "qc.batch.held"
	default:
		return ""
	}
}

const releaseCols = `business_id, decision_date::text, batch_ref, product, check_refs, decided_by,
	decision, status, notes, attachments, attrs, created_at, updated_at`

func (s *Store) ListReleaseDecisions(ctx context.Context, batchRef, decision string, limit int) ([]ReleaseDecision, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+releaseCols+`
		FROM qc_release_decisions
		WHERE ($1 = '' OR batch_ref = $1) AND ($2 = '' OR decision = $2)
		ORDER BY decision_date DESC NULLS LAST, updated_at DESC
		LIMIT $3`, strings.TrimSpace(batchRef), NormalizeReleaseDecision(decision), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ReleaseDecision{}
	for rows.Next() {
		item, _, err := scanReleaseDecision(rows, false)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

/*
UpsertReleaseDecision saves a decision and reports whether it is worth announcing.

The second return value is true only when the row is new or its decision code
actually changed. That matters because this is an upsert — the Lab app's adapter
turns an edit into the same POST — so correcting a typo in the notes of an
already-released batch would otherwise re-emit qc.batch.released and re-trigger
real work in warehouse and traceability. A batch is released once.

The previous decision is read in the same statement as the write, so the
comparison cannot race another writer.
*/
func (s *Store) UpsertReleaseDecision(ctx context.Context, in UpsertReleaseDecisionInput) (ReleaseDecision, bool, error) {
	batchRef := strings.TrimSpace(in.BatchRef)
	if batchRef == "" {
		return ReleaseDecision{}, false, ErrBadInput
	}
	id := strings.TrimSpace(in.BusinessID)
	if id == "" {
		var err error
		if id, err = nextBusinessID(ctx, s.pool, "qc_release_decisions", "REL"); err != nil {
			return ReleaseDecision{}, false, err
		}
	}
	var decision *string
	if in.Decision != nil {
		normalised := NormalizeReleaseDecision(*in.Decision)
		decision = blankToNil(&normalised)
	}
	rows, err := s.pool.Query(ctx, `
		WITH prev AS (
			SELECT decision FROM qc_release_decisions WHERE business_id = $1
		), up AS (
			INSERT INTO qc_release_decisions (
				business_id, decision_date, batch_ref, product, check_refs, decided_by,
				decision, status, notes, attachments, attrs, updated_at
			) VALUES (
				$1, COALESCE(NULLIF($2::text, '')::date, CURRENT_DATE), $3, COALESCE($4::text, ''),
				COALESCE($5::text, ''), COALESCE($6::text, ''), COALESCE($7::text, ''),
				COALESCE($8::text, 'draft'), COALESCE($9::text, ''), COALESCE($10::text, ''),
				COALESCE($11::jsonb, '{}'::jsonb), NOW()
			)
			ON CONFLICT (business_id) DO UPDATE SET
				decision_date = CASE WHEN $2::text IS NULL THEN qc_release_decisions.decision_date
				                     WHEN $2::text = '' THEN NULL
				                     ELSE $2::date END,
				batch_ref = EXCLUDED.batch_ref,
				product = COALESCE($4::text, qc_release_decisions.product),
				check_refs = COALESCE($5::text, qc_release_decisions.check_refs),
				decided_by = COALESCE($6::text, qc_release_decisions.decided_by),
				decision = COALESCE($7::text, qc_release_decisions.decision),
				status = COALESCE($8::text, qc_release_decisions.status),
				notes = COALESCE($9::text, qc_release_decisions.notes),
				attachments = COALESCE($10::text, qc_release_decisions.attachments),
				attrs = CASE WHEN $11::jsonb IS NULL THEN qc_release_decisions.attrs
				             ELSE qc_release_decisions.attrs || $11::jsonb END,
				updated_at = NOW()
			RETURNING `+releaseCols+`
		)
		SELECT up.*, COALESCE((SELECT decision FROM prev), '') AS prev_decision FROM up`,
		id, in.DecisionDate, batchRef, trimOptional(in.Product), trimOptional(in.CheckRefs),
		trimOptional(in.DecidedBy), decision, blankToNil(in.Status), in.Notes,
		in.Attachments, attrsOptional(in.Attrs),
	)
	if err != nil {
		return ReleaseDecision{}, false, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return ReleaseDecision{}, false, err
		}
		return ReleaseDecision{}, false, ErrNotFound
	}
	item, prev, err := scanReleaseDecision(rows, true)
	if err != nil {
		return ReleaseDecision{}, false, err
	}
	return item, item.Decision != "" && item.Decision != prev, nil
}

func scanReleaseDecision(rows interface{ Scan(...any) error }, withPrev bool) (ReleaseDecision, string, error) {
	var item ReleaseDecision
	var attrs []byte
	var prev string
	targets := []any{&item.BusinessID, &item.DecisionDate, &item.BatchRef, &item.Product,
		&item.CheckRefs, &item.DecidedBy, &item.Decision, &item.Status, &item.Notes,
		&item.Attachments, &attrs, &item.CreatedAt, &item.UpdatedAt}
	if withPrev {
		targets = append(targets, &prev)
	}
	if err := rows.Scan(targets...); err != nil {
		return ReleaseDecision{}, "", err
	}
	item.Attrs = attrsMap(attrs)
	return item, prev, nil
}
