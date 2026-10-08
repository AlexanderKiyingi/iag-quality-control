package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *Store) CreateSample(ctx context.Context, in CreateSampleInput) (Sample, error) {
	bid := strings.TrimSpace(in.BatchBusinessID)
	if bid == "" {
		return Sample{}, ErrBadInput
	}
	businessID := strings.TrimSpace(in.SampleID)
	if businessID == "" {
		id, err := nextBusinessID(ctx, s.pool, "qc_samples", "SMP")
		if err != nil {
			return Sample{}, err
		}
		businessID = id
	}
	tests := in.TestsRequired
	if tests == nil {
		tests = []string{}
	}
	testsJSON, err := json.Marshal(tests)
	if err != nil {
		return Sample{}, err
	}
	priority := strings.TrimSpace(in.Priority)
	if priority == "" {
		priority = "normal"
	}
	status := "pending"
	if priority == "urgent" {
		status = "urgent"
	}
	var out Sample
	var attrs []byte
	err = s.pool.QueryRow(ctx, `
		INSERT INTO qc_samples (
			business_id, batch_business_id, sample_type, status, priority,
			assigned_tech, tests_required, notes, attrs
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,COALESCE($9::jsonb,'{}'::jsonb))
		RETURNING business_id, batch_business_id, sample_type, status, priority,
		          assigned_tech, tests_required, notes, attrs, received_at, completed_at, created_at`,
		businessID, bid, strings.TrimSpace(in.SampleType), status, priority,
		strings.TrimSpace(in.AssignedTech), testsJSON, strings.TrimSpace(in.Notes),
		attrsOptional(in.Attrs),
	).Scan(
		&out.BusinessID, &out.BatchBusinessID, &out.SampleType, &out.Status, &out.Priority,
		&out.AssignedTech, &testsJSON, &out.Notes, &attrs, &out.ReceivedAt, &out.CompletedAt, &out.CreatedAt,
	)
	if err != nil {
		return Sample{}, fmt.Errorf("create sample: %w", err)
	}
	_ = json.Unmarshal(testsJSON, &out.TestsRequired)
	out.Attrs = attrsMap(attrs)
	return out, nil
}

func (s *Store) GetSample(ctx context.Context, businessID string) (Sample, error) {
	var out Sample
	var testsJSON []byte
	var attrs []byte
	err := s.pool.QueryRow(ctx, `
		SELECT business_id, batch_business_id, sample_type, status, priority,
		       assigned_tech, tests_required, notes, attrs, received_at, completed_at, created_at
		FROM qc_samples WHERE business_id = $1`, businessID,
	).Scan(
		&out.BusinessID, &out.BatchBusinessID, &out.SampleType, &out.Status, &out.Priority,
		&out.AssignedTech, &testsJSON, &out.Notes, &attrs, &out.ReceivedAt, &out.CompletedAt, &out.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Sample{}, ErrNotFound
	}
	if err != nil {
		return Sample{}, err
	}
	_ = json.Unmarshal(testsJSON, &out.TestsRequired)
	out.Attrs = attrsMap(attrs)
	return out, nil
}

func (s *Store) ListSamples(ctx context.Context, status string, batchID string, limit int) ([]Sample, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := `
		SELECT business_id, batch_business_id, sample_type, status, priority,
		       assigned_tech, tests_required, notes, attrs, received_at, completed_at, created_at
		FROM qc_samples WHERE 1=1`
	args := []any{}
	n := 1
	if status != "" && status != "all" {
		q += fmt.Sprintf(" AND status = $%d", n)
		args = append(args, status)
		n++
	}
	if batchID != "" {
		q += fmt.Sprintf(" AND batch_business_id = $%d", n)
		args = append(args, batchID)
		n++
	}
	q += fmt.Sprintf(" ORDER BY received_at DESC LIMIT $%d", n)
	args = append(args, limit)

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Sample
	for rows.Next() {
		var item Sample
		var testsJSON []byte
		var attrs []byte
		if err := rows.Scan(
			&item.BusinessID, &item.BatchBusinessID, &item.SampleType, &item.Status, &item.Priority,
			&item.AssignedTech, &testsJSON, &item.Notes, &attrs, &item.ReceivedAt, &item.CompletedAt, &item.CreatedAt,
		); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(testsJSON, &item.TestsRequired)
		item.Attrs = attrsMap(attrs)
		out = append(out, item)
	}
	return out, rows.Err()
}

var allowedSampleStatuses = map[string]bool{
	"pending": true, "urgent": true, "in-progress": true, "complete": true, "retest": true,
}

// SamplePatch is a partial update: nil leaves the column alone. Status is
// validated against allowedSampleStatuses when set.
type SamplePatch struct {
	Status       *string
	SampleType   *string
	Priority     *string
	AssignedTech *string
	Notes        *string
	Attrs        map[string]any
}

// UpdateSampleStatus moves a sample's status. The tests and cupping flows
// call it; PATCH /samples/:id goes through UpdateSample so a client can edit
// the other columns without pretending they are part of the notes.
func (s *Store) UpdateSampleStatus(ctx context.Context, businessID, status string) (Sample, error) {
	return s.UpdateSample(ctx, businessID, SamplePatch{Status: &status})
}

// UpdateSample applies a SamplePatch. At least one field must be set. Moving to
// "complete" stamps completed_at, as it always has; every other column is
// COALESCEd so an omitted field is left as it was.
func (s *Store) UpdateSample(ctx context.Context, businessID string, p SamplePatch) (Sample, error) {
	var status *string
	if p.Status != nil {
		trimmed := strings.TrimSpace(*p.Status)
		if trimmed == "" || !allowedSampleStatuses[trimmed] {
			return Sample{}, ErrBadInput
		}
		status = &trimmed
	}
	if status == nil && p.SampleType == nil && p.Priority == nil && p.AssignedTech == nil &&
		p.Notes == nil && p.Attrs == nil {
		return Sample{}, ErrBadInput
	}
	var completedAt *time.Time
	if status != nil && *status == "complete" {
		now := time.Now().UTC()
		completedAt = &now
	}
	var out Sample
	var testsJSON []byte
	var attrs []byte
	err := s.pool.QueryRow(ctx, `
		UPDATE qc_samples
		SET status        = COALESCE($2, status),
		    completed_at  = COALESCE($3, completed_at),
		    sample_type   = COALESCE(NULLIF(TRIM($4), ''), sample_type),
		    priority      = COALESCE(NULLIF(TRIM($5), ''), priority),
		    assigned_tech = COALESCE($6, assigned_tech),
		    notes         = COALESCE($7, notes),
		    attrs         = CASE WHEN $8::jsonb IS NULL THEN qc_samples.attrs
		                        ELSE qc_samples.attrs || $8::jsonb END
		WHERE business_id = $1
		RETURNING business_id, batch_business_id, sample_type, status, priority,
		          assigned_tech, tests_required, notes, attrs, received_at, completed_at, created_at`,
		businessID, status, completedAt, p.SampleType, p.Priority, p.AssignedTech, p.Notes,
		attrsOptional(p.Attrs),
	).Scan(
		&out.BusinessID, &out.BatchBusinessID, &out.SampleType, &out.Status, &out.Priority,
		&out.AssignedTech, &testsJSON, &out.Notes, &attrs, &out.ReceivedAt, &out.CompletedAt, &out.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Sample{}, ErrNotFound
	}
	if err != nil {
		return Sample{}, err
	}
	_ = json.Unmarshal(testsJSON, &out.TestsRequired)
	out.Attrs = attrsMap(attrs)
	return out, nil
}
