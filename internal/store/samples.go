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
	err = s.pool.QueryRow(ctx, `
		INSERT INTO qc_samples (
			business_id, batch_business_id, sample_type, status, priority,
			assigned_tech, tests_required, notes
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		RETURNING business_id, batch_business_id, sample_type, status, priority,
		          assigned_tech, tests_required, notes, received_at, completed_at, created_at`,
		businessID, bid, strings.TrimSpace(in.SampleType), status, priority,
		strings.TrimSpace(in.AssignedTech), testsJSON, strings.TrimSpace(in.Notes),
	).Scan(
		&out.BusinessID, &out.BatchBusinessID, &out.SampleType, &out.Status, &out.Priority,
		&out.AssignedTech, &testsJSON, &out.Notes, &out.ReceivedAt, &out.CompletedAt, &out.CreatedAt,
	)
	if err != nil {
		return Sample{}, fmt.Errorf("create sample: %w", err)
	}
	_ = json.Unmarshal(testsJSON, &out.TestsRequired)
	return out, nil
}

func (s *Store) GetSample(ctx context.Context, businessID string) (Sample, error) {
	var out Sample
	var testsJSON []byte
	err := s.pool.QueryRow(ctx, `
		SELECT business_id, batch_business_id, sample_type, status, priority,
		       assigned_tech, tests_required, notes, received_at, completed_at, created_at
		FROM qc_samples WHERE business_id = $1`, businessID,
	).Scan(
		&out.BusinessID, &out.BatchBusinessID, &out.SampleType, &out.Status, &out.Priority,
		&out.AssignedTech, &testsJSON, &out.Notes, &out.ReceivedAt, &out.CompletedAt, &out.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Sample{}, ErrNotFound
	}
	if err != nil {
		return Sample{}, err
	}
	_ = json.Unmarshal(testsJSON, &out.TestsRequired)
	return out, nil
}

func (s *Store) ListSamples(ctx context.Context, status string, batchID string, limit int) ([]Sample, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := `
		SELECT business_id, batch_business_id, sample_type, status, priority,
		       assigned_tech, tests_required, notes, received_at, completed_at, created_at
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
		if err := rows.Scan(
			&item.BusinessID, &item.BatchBusinessID, &item.SampleType, &item.Status, &item.Priority,
			&item.AssignedTech, &testsJSON, &item.Notes, &item.ReceivedAt, &item.CompletedAt, &item.CreatedAt,
		); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(testsJSON, &item.TestsRequired)
		out = append(out, item)
	}
	return out, rows.Err()
}

var allowedSampleStatuses = map[string]bool{
	"pending": true, "urgent": true, "in-progress": true, "complete": true, "retest": true,
}

func (s *Store) UpdateSampleStatus(ctx context.Context, businessID, status string) (Sample, error) {
	status = strings.TrimSpace(status)
	if status == "" || !allowedSampleStatuses[status] {
		return Sample{}, ErrBadInput
	}
	var completedAt *time.Time
	if status == "complete" {
		now := time.Now().UTC()
		completedAt = &now
	}
	var out Sample
	var testsJSON []byte
	err := s.pool.QueryRow(ctx, `
		UPDATE qc_samples
		SET status = $2, completed_at = COALESCE($3, completed_at)
		WHERE business_id = $1
		RETURNING business_id, batch_business_id, sample_type, status, priority,
		          assigned_tech, tests_required, notes, received_at, completed_at, created_at`,
		businessID, status, completedAt,
	).Scan(
		&out.BusinessID, &out.BatchBusinessID, &out.SampleType, &out.Status, &out.Priority,
		&out.AssignedTech, &testsJSON, &out.Notes, &out.ReceivedAt, &out.CompletedAt, &out.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Sample{}, ErrNotFound
	}
	if err != nil {
		return Sample{}, err
	}
	_ = json.Unmarshal(testsJSON, &out.TestsRequired)
	return out, nil
}
