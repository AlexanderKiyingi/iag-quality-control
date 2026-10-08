package store

import (
	"context"
	"strings"
	"time"
)

// NonConformance is a quality finding (migration 012).
//
// Separate from CAPA on purpose: an NC is the finding, a CAPA is the response,
// and qc_capas.source_ref carries this record's business_id to link them.
type NonConformance struct {
	BusinessID  string         `json:"business_id"`
	RaisedDate  *string        `json:"raised_date,omitempty"`
	SourceRef   string         `json:"source_ref"`
	Title       string         `json:"title"`
	Severity    string         `json:"severity"`
	Owner       string         `json:"owner"`
	Status      string         `json:"status"`
	Description string         `json:"description"`
	RootCause   string         `json:"root_cause"`
	Attachments string         `json:"attachments"`
	Attrs       map[string]any `json:"attrs"`
	// AutoSource is the record whose failed specification raised this NC
	// (016), or "" for one a person raised.
	AutoSource string    `json:"auto_source"`
	UpdatedBy  string    `json:"updated_by"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// UpsertNonConformanceInput is the write shape. Title is required; the optional
// fields follow the nil/""/value contract in optional.go.
type UpsertNonConformanceInput struct {
	BusinessID  string
	Title       string
	RaisedDate  *string
	SourceRef   *string
	Severity    *string
	Owner       *string
	Status      *string
	Description *string
	RootCause   *string
	Attachments *string
	Attrs       map[string]any
	Actor       string
}

const nonConformanceCols = `business_id, raised_date::text, source_ref, title, severity, owner,
	status, description, root_cause, attachments, attrs, auto_source, updated_by,
	created_at, updated_at`

func (s *Store) ListNonConformances(ctx context.Context, status, severity string, limit int) ([]NonConformance, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+nonConformanceCols+`
		FROM qc_non_conformances
		WHERE ($1 = '' OR status = $1) AND ($2 = '' OR severity = $2)
		ORDER BY raised_date DESC NULLS LAST, updated_at DESC
		LIMIT $3`, strings.TrimSpace(status), strings.TrimSpace(severity), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []NonConformance{}
	for rows.Next() {
		item, err := scanNonConformance(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) GetNonConformance(ctx context.Context, businessID string) (NonConformance, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+nonConformanceCols+`
		FROM qc_non_conformances WHERE business_id = $1`, strings.TrimSpace(businessID))
	if err != nil {
		return NonConformance{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		return NonConformance{}, ErrNotFound
	}
	return scanNonConformance(rows)
}

// UpsertNonConformance creates or updates an NC by business_id, preserving any
// column the caller did not send.
func (s *Store) UpsertNonConformance(ctx context.Context, in UpsertNonConformanceInput) (NonConformance, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return NonConformance{}, ErrBadInput
	}
	id := strings.TrimSpace(in.BusinessID)
	if id == "" {
		var err error
		if id, err = nextBusinessID(ctx, s.pool, "qc_non_conformances", "NCR"); err != nil {
			return NonConformance{}, err
		}
	}
	rows, err := s.pool.Query(ctx, `
		INSERT INTO qc_non_conformances (
			business_id, raised_date, source_ref, title, severity, owner,
			status, description, root_cause, attachments, attrs, updated_by, updated_at
		) VALUES (
			$1, COALESCE(NULLIF($2::text, '')::date, CURRENT_DATE), COALESCE($3::text, ''), $4,
			COALESCE($5::text, 'minor'), COALESCE($6::text, ''), COALESCE($7::text, 'open'),
			COALESCE($8::text, ''), COALESCE($9::text, ''), COALESCE($10::text, ''),
			COALESCE($11::jsonb, '{}'::jsonb), $12, NOW()
		)
		ON CONFLICT (business_id) DO UPDATE SET
			raised_date = CASE WHEN $2::text IS NULL THEN qc_non_conformances.raised_date
			                   WHEN $2::text = '' THEN NULL
			                   ELSE $2::date END,
			source_ref = COALESCE($3::text, qc_non_conformances.source_ref),
			title = EXCLUDED.title,
			severity = COALESCE($5::text, qc_non_conformances.severity),
			owner = COALESCE($6::text, qc_non_conformances.owner),
			status = COALESCE($7::text, qc_non_conformances.status),
			description = COALESCE($8::text, qc_non_conformances.description),
			root_cause = COALESCE($9::text, qc_non_conformances.root_cause),
			attachments = COALESCE($10::text, qc_non_conformances.attachments),
			attrs = CASE WHEN $11::jsonb IS NULL THEN qc_non_conformances.attrs
			             ELSE qc_non_conformances.attrs || $11::jsonb END,
			updated_by = EXCLUDED.updated_by,
			updated_at = NOW()
		RETURNING `+nonConformanceCols,
		id, in.RaisedDate, trimOptional(in.SourceRef), title, blankToNil(in.Severity),
		trimOptional(in.Owner), blankToNil(in.Status), in.Description, in.RootCause,
		in.Attachments, attrsOptional(in.Attrs), strings.TrimSpace(in.Actor),
	)
	if err != nil {
		return NonConformance{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return NonConformance{}, err
		}
		return NonConformance{}, ErrNotFound
	}
	return scanNonConformance(rows)
}

func scanNonConformance(rows interface{ Scan(...any) error }) (NonConformance, error) {
	var item NonConformance
	var attrs []byte
	if err := rows.Scan(&item.BusinessID, &item.RaisedDate, &item.SourceRef, &item.Title,
		&item.Severity, &item.Owner, &item.Status, &item.Description, &item.RootCause,
		&item.Attachments, &attrs, &item.AutoSource, &item.UpdatedBy,
		&item.CreatedAt, &item.UpdatedAt); err != nil {
		return NonConformance{}, err
	}
	item.Attrs = attrsMap(attrs)
	return item, nil
}
