package store

import (
	"context"
	"strings"
	"time"
)

// StabilityStudy is a storage-condition study over time (migration 011).
//
// One next_pull_date, not a child table of pull points — see the migration
// header for why, and for the additive follow-up when the UI grows one.
type StabilityStudy struct {
	BusinessID       string         `json:"business_id"`
	StartDate        *string        `json:"start_date,omitempty"`
	Product          string         `json:"product"`
	StorageCondition string         `json:"storage_condition"`
	DurationDays     int            `json:"duration_days"`
	NextPullDate     *string        `json:"next_pull_date,omitempty"`
	TestsScheduled   string         `json:"tests_scheduled"`
	Owner            string         `json:"owner"`
	Status           string         `json:"status"`
	Notes            string         `json:"notes"`
	Attachments      string         `json:"attachments"`
	Attrs            map[string]any `json:"attrs"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
}

// UpsertStabilityStudyInput is the write shape. Product is required; the
// optional fields follow the nil/""/value contract in optional.go.
type UpsertStabilityStudyInput struct {
	BusinessID       string
	Product          string
	StartDate        *string
	StorageCondition *string
	DurationDays     *int
	NextPullDate     *string
	TestsScheduled   *string
	Owner            *string
	Status           *string
	Notes            *string
	Attachments      *string
	Attrs            map[string]any
}

const stabilityCols = `business_id, start_date::text, product, storage_condition, duration_days,
	next_pull_date::text, tests_scheduled, owner, status, notes, attachments, attrs,
	created_at, updated_at`

func (s *Store) ListStabilityStudies(ctx context.Context, status string, limit int) ([]StabilityStudy, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+stabilityCols+`
		FROM qc_stability_studies
		WHERE ($1 = '' OR status = $1)
		ORDER BY next_pull_date NULLS LAST, updated_at DESC
		LIMIT $2`, strings.TrimSpace(status), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []StabilityStudy{}
	for rows.Next() {
		item, err := scanStabilityStudy(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// UpsertStabilityStudy creates or updates a study by business_id, preserving
// any column the caller did not send. See optional.go for the contract.
func (s *Store) UpsertStabilityStudy(ctx context.Context, in UpsertStabilityStudyInput) (StabilityStudy, error) {
	product := strings.TrimSpace(in.Product)
	if product == "" {
		return StabilityStudy{}, ErrBadInput
	}
	id := strings.TrimSpace(in.BusinessID)
	if id == "" {
		var err error
		if id, err = nextBusinessID(ctx, s.pool, "qc_stability_studies", "STB"); err != nil {
			return StabilityStudy{}, err
		}
	}
	rows, err := s.pool.Query(ctx, `
		INSERT INTO qc_stability_studies (
			business_id, start_date, product, storage_condition, duration_days,
			next_pull_date, tests_scheduled, owner, status, notes, attachments, attrs, updated_at
		) VALUES (
			$1, NULLIF($2::text, '')::date, $3, COALESCE($4::text, ''), COALESCE($5::int, 0),
			NULLIF($6::text, '')::date, COALESCE($7::text, ''), COALESCE($8::text, ''),
			COALESCE($9::text, 'planned'), COALESCE($10::text, ''), COALESCE($11::text, ''),
			COALESCE($12::jsonb, '{}'::jsonb), NOW()
		)
		ON CONFLICT (business_id) DO UPDATE SET
			start_date = CASE WHEN $2::text IS NULL THEN qc_stability_studies.start_date
			                  WHEN $2::text = '' THEN NULL
			                  ELSE $2::date END,
			product = EXCLUDED.product,
			storage_condition = COALESCE($4::text, qc_stability_studies.storage_condition),
			duration_days = COALESCE($5::int, qc_stability_studies.duration_days),
			next_pull_date = CASE WHEN $6::text IS NULL THEN qc_stability_studies.next_pull_date
			                      WHEN $6::text = '' THEN NULL
			                      ELSE $6::date END,
			tests_scheduled = COALESCE($7::text, qc_stability_studies.tests_scheduled),
			owner = COALESCE($8::text, qc_stability_studies.owner),
			status = COALESCE($9::text, qc_stability_studies.status),
			notes = COALESCE($10::text, qc_stability_studies.notes),
			attachments = COALESCE($11::text, qc_stability_studies.attachments),
			attrs = CASE WHEN $12::jsonb IS NULL THEN qc_stability_studies.attrs
			             ELSE qc_stability_studies.attrs || $12::jsonb END,
			updated_at = NOW()
		RETURNING `+stabilityCols,
		id, in.StartDate, product, trimOptional(in.StorageCondition), in.DurationDays,
		in.NextPullDate, trimOptional(in.TestsScheduled), trimOptional(in.Owner),
		blankToNil(in.Status), in.Notes, in.Attachments, attrsOptional(in.Attrs),
	)
	if err != nil {
		return StabilityStudy{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return StabilityStudy{}, err
		}
		return StabilityStudy{}, ErrNotFound
	}
	return scanStabilityStudy(rows)
}

func scanStabilityStudy(rows interface{ Scan(...any) error }) (StabilityStudy, error) {
	var item StabilityStudy
	var attrs []byte
	if err := rows.Scan(&item.BusinessID, &item.StartDate, &item.Product, &item.StorageCondition,
		&item.DurationDays, &item.NextPullDate, &item.TestsScheduled, &item.Owner, &item.Status,
		&item.Notes, &item.Attachments, &attrs, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return StabilityStudy{}, err
	}
	item.Attrs = attrsMap(attrs)
	return item, nil
}
