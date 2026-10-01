package store

import (
	"context"
	"strings"
	"time"
)

// IncomingInspection is a supplier lot checked on arrival (migration 015).
//
// Its own register, not qc_in_process_checks: that one is per-batch process
// stages, and sharing a table would mix supplier receipts with production
// checks in one list.
type IncomingInspection struct {
	BusinessID  string         `json:"business_id"`
	InspectedAt *string        `json:"inspected_at,omitempty"`
	SourceLot   string         `json:"source_lot"`
	ItemRef     string         `json:"item_ref"`
	SampleSize  int            `json:"sample_size"`
	MoisturePct *float64       `json:"moisture_pct,omitempty"`
	DefectCount int            `json:"defect_count"`
	Inspector   string         `json:"inspector"`
	Result      string         `json:"result"`
	Status      string         `json:"status"`
	Notes       string         `json:"notes"`
	Attachments string         `json:"attachments"`
	Attrs       map[string]any `json:"attrs"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

// UpsertIncomingInspectionInput is the write shape. SourceLot is required — an
// inspection that names no lot cannot be traced to what arrived. The optional
// fields follow the nil/""/value contract in optional.go.
type UpsertIncomingInspectionInput struct {
	BusinessID  string
	SourceLot   string
	InspectedAt *string
	ItemRef     *string
	SampleSize  *int
	MoisturePct *float64
	DefectCount *int
	Inspector   *string
	Result      *string
	Status      *string
	Notes       *string
	Attachments *string
	Attrs       map[string]any
}

const incomingInspectionCols = `business_id, inspected_at::text, source_lot, item_ref, sample_size,
	moisture_pct, defect_count, inspector, result, status, notes, attachments, attrs,
	created_at, updated_at`

func (s *Store) ListIncomingInspections(ctx context.Context, sourceLot, status string, limit int) ([]IncomingInspection, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+incomingInspectionCols+`
		FROM qc_incoming_inspections
		WHERE ($1 = '' OR source_lot = $1) AND ($2 = '' OR status = $2)
		ORDER BY inspected_at DESC NULLS LAST, updated_at DESC
		LIMIT $3`, strings.TrimSpace(sourceLot), strings.TrimSpace(status), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []IncomingInspection{}
	for rows.Next() {
		item, err := scanIncomingInspection(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// UpsertIncomingInspection creates or updates an inspection by business_id,
// preserving any column the caller did not send.
func (s *Store) UpsertIncomingInspection(ctx context.Context, in UpsertIncomingInspectionInput) (IncomingInspection, error) {
	sourceLot := strings.TrimSpace(in.SourceLot)
	if sourceLot == "" {
		return IncomingInspection{}, ErrBadInput
	}
	id := strings.TrimSpace(in.BusinessID)
	if id == "" {
		var err error
		if id, err = nextBusinessID(ctx, s.pool, "qc_incoming_inspections", "IIN"); err != nil {
			return IncomingInspection{}, err
		}
	}
	rows, err := s.pool.Query(ctx, `
		INSERT INTO qc_incoming_inspections (
			business_id, inspected_at, source_lot, item_ref, sample_size, moisture_pct,
			defect_count, inspector, result, status, notes, attachments, attrs, updated_at
		) VALUES (
			$1, COALESCE(NULLIF($2::text, '')::date, CURRENT_DATE), $3, COALESCE($4::text, ''),
			COALESCE($5::int, 0), $6, COALESCE($7::int, 0), COALESCE($8::text, ''),
			COALESCE($9::text, ''), COALESCE($10::text, 'open'), COALESCE($11::text, ''),
			COALESCE($12::text, ''), COALESCE($13::jsonb, '{}'::jsonb), NOW()
		)
		ON CONFLICT (business_id) DO UPDATE SET
			inspected_at = CASE WHEN $2::text IS NULL THEN qc_incoming_inspections.inspected_at
			                    WHEN $2::text = '' THEN NULL
			                    ELSE $2::date END,
			source_lot = EXCLUDED.source_lot,
			item_ref = COALESCE($4::text, qc_incoming_inspections.item_ref),
			sample_size = COALESCE($5::int, qc_incoming_inspections.sample_size),
			moisture_pct = COALESCE($6::double precision, qc_incoming_inspections.moisture_pct),
			defect_count = COALESCE($7::int, qc_incoming_inspections.defect_count),
			inspector = COALESCE($8::text, qc_incoming_inspections.inspector),
			result = COALESCE($9::text, qc_incoming_inspections.result),
			status = COALESCE($10::text, qc_incoming_inspections.status),
			notes = COALESCE($11::text, qc_incoming_inspections.notes),
			attachments = COALESCE($12::text, qc_incoming_inspections.attachments),
			attrs = CASE WHEN $13::jsonb IS NULL THEN qc_incoming_inspections.attrs
			             ELSE qc_incoming_inspections.attrs || $13::jsonb END,
			updated_at = NOW()
		RETURNING `+incomingInspectionCols,
		id, in.InspectedAt, sourceLot, trimOptional(in.ItemRef), in.SampleSize, in.MoisturePct,
		in.DefectCount, trimOptional(in.Inspector), trimOptional(in.Result),
		blankToNil(in.Status), in.Notes, in.Attachments, attrsOptional(in.Attrs),
	)
	if err != nil {
		return IncomingInspection{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return IncomingInspection{}, err
		}
		return IncomingInspection{}, ErrNotFound
	}
	return scanIncomingInspection(rows)
}

func scanIncomingInspection(rows interface{ Scan(...any) error }) (IncomingInspection, error) {
	var item IncomingInspection
	var attrs []byte
	if err := rows.Scan(&item.BusinessID, &item.InspectedAt, &item.SourceLot, &item.ItemRef,
		&item.SampleSize, &item.MoisturePct, &item.DefectCount, &item.Inspector, &item.Result,
		&item.Status, &item.Notes, &item.Attachments, &attrs,
		&item.CreatedAt, &item.UpdatedAt); err != nil {
		return IncomingInspection{}, err
	}
	item.Attrs = attrsMap(attrs)
	return item, nil
}
