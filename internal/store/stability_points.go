package store

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"iag-quality-control/backend/internal/domain"
)

// PullPoint is one timepoint of a stability study (migration 024).
type PullPoint struct {
	StudyBusinessID string            `json:"study_business_id"`
	PullDay         int               `json:"pull_day"`
	DueDate         string            `json:"due_date"`
	Status          string            `json:"status"`
	Overdue         bool              `json:"overdue"`
	TestedAt        *time.Time        `json:"tested_at,omitempty"`
	Notes           string            `json:"notes"`
	Results         []StabilityResult `json:"results"`
}

// StabilityResult is one reading at one timepoint.
type StabilityResult struct {
	ID         int64     `json:"id"`
	PullDay    int       `json:"pull_day"`
	Parameter  string    `json:"parameter"`
	Value      float64   `json:"value"`
	Unit       string    `json:"unit"`
	RecordedBy string    `json:"recorded_by"`
	Notes      string    `json:"notes"`
	RecordedAt time.Time `json:"recorded_at"`
}

// StabilityReading is a write: one parameter's value at a timepoint.
type StabilityReading struct {
	Parameter string  `json:"parameter"`
	Value     float64 `json:"value"`
	Unit      string  `json:"unit"`
}

/*
SchedulePullPoints lays out a study's timepoints as pull points due on start
date + day. Points already scheduled keep their status and results; a pending
point no longer in the list is cancelled, not deleted, so the schedule's
history stays readable. The study needs a start date to schedule from.
*/
func (s *Store) SchedulePullPoints(ctx context.Context, study string, days []int) ([]PullPoint, error) {
	study = strings.TrimSpace(study)
	var start *string
	err := s.pool.QueryRow(ctx, `SELECT start_date::text FROM qc_stability_studies WHERE business_id = $1`, study).Scan(&start)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if start == nil || *start == "" {
		return nil, fmt.Errorf("%w: give the study a start date before scheduling pulls", ErrBadInput)
	}
	seen := map[int]bool{}
	clean := []int{}
	for _, d := range days {
		if d < 0 || d > 3650 {
			return nil, fmt.Errorf("%w: timepoints are days 0 – 3650", ErrBadInput)
		}
		if !seen[d] {
			seen[d] = true
			clean = append(clean, d)
		}
	}
	if len(clean) == 0 {
		return nil, fmt.Errorf("%w: give at least one timepoint", ErrBadInput)
	}
	sort.Ints(clean)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, d := range clean {
		if _, err := tx.Exec(ctx, `
			INSERT INTO qc_stability_pull_points (study_business_id, pull_day, due_date)
			VALUES ($1, $2::int, $3::date + $2::int)
			ON CONFLICT (study_business_id, pull_day) DO UPDATE SET
				status = CASE WHEN qc_stability_pull_points.status = 'cancelled' THEN 'pending'
				              ELSE qc_stability_pull_points.status END`, study, d, *start); err != nil {
			return nil, err
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE qc_stability_pull_points SET status = 'cancelled'
		WHERE study_business_id = $1 AND status = 'pending' AND NOT (pull_day = ANY($2))`, study, clean); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE qc_stability_studies SET next_pull_date = (
			SELECT min(due_date) FROM qc_stability_pull_points WHERE study_business_id = $1 AND status = 'pending'),
			duration_days = GREATEST(duration_days, $2), updated_at = NOW()
		WHERE business_id = $1`, study, clean[len(clean)-1]); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.ListPullPoints(ctx, study)
}

// ListPullPoints returns a study's timepoints with their results.
func (s *Store) ListPullPoints(ctx context.Context, study string) ([]PullPoint, error) {
	study = strings.TrimSpace(study)
	rows, err := s.pool.Query(ctx, `
		SELECT study_business_id, pull_day, due_date::text, status, status = 'pending' AND due_date < CURRENT_DATE,
		       tested_at, notes
		FROM qc_stability_pull_points WHERE study_business_id = $1 ORDER BY pull_day`, study)
	if err != nil {
		return nil, err
	}
	out := []PullPoint{}
	index := map[int]int{}
	for rows.Next() {
		var p PullPoint
		if err := rows.Scan(&p.StudyBusinessID, &p.PullDay, &p.DueDate, &p.Status, &p.Overdue, &p.TestedAt, &p.Notes); err != nil {
			rows.Close()
			return nil, err
		}
		p.Results = []StabilityResult{}
		index[p.PullDay] = len(out)
		out = append(out, p)
	}
	rows.Close()
	results, err := s.stabilityResults(ctx, study, "")
	if err != nil {
		return nil, err
	}
	for _, r := range results {
		if i, ok := index[r.PullDay]; ok {
			out[i].Results = append(out[i].Results, r)
		}
	}
	return out, nil
}

func (s *Store) stabilityResults(ctx context.Context, study, parameter string) ([]StabilityResult, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, pull_day, parameter, value, unit, recorded_by, notes, recorded_at
		FROM qc_stability_results WHERE study_business_id = $1 AND ($2 = '' OR parameter = $2)
		ORDER BY pull_day, id`, study, parameter)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []StabilityResult{}
	for rows.Next() {
		var r StabilityResult
		if err := rows.Scan(&r.ID, &r.PullDay, &r.Parameter, &r.Value, &r.Unit, &r.RecordedBy, &r.Notes, &r.RecordedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// RecordStabilityResults files readings at a scheduled timepoint and marks it
// tested. Parameters are stored by their canonical name so the shelf-life fit
// and the spec lookup agree on what "Moisture %" is.
func (s *Store) RecordStabilityResults(ctx context.Context, study string, day int, readings []StabilityReading, notes, actor string) ([]PullPoint, error) {
	study = strings.TrimSpace(study)
	if len(readings) == 0 {
		return nil, fmt.Errorf("%w: record at least one result", ErrBadInput)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var status string
	err = tx.QueryRow(ctx, `SELECT status FROM qc_stability_pull_points WHERE study_business_id = $1 AND pull_day = $2 FOR UPDATE`,
		study, day).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w: day %d is not a scheduled pull of %s", ErrNotFound, day, study)
	}
	if err != nil {
		return nil, err
	}
	if status == "cancelled" {
		return nil, fmt.Errorf("%w: the day %d pull was cancelled", ErrConflict, day)
	}
	for _, r := range readings {
		parameter := CanonicalParameter(r.Parameter)
		if parameter == "" {
			return nil, fmt.Errorf("%w: every result names its parameter", ErrBadInput)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO qc_stability_results (study_business_id, pull_day, parameter, value, unit, recorded_by, notes)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`, study, day, parameter, r.Value, strings.TrimSpace(r.Unit),
			strings.TrimSpace(actor), strings.TrimSpace(notes)); err != nil {
			return nil, err
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE qc_stability_pull_points SET status = 'tested', tested_at = COALESCE(tested_at, NOW())
		WHERE study_business_id = $1 AND pull_day = $2`, study, day); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE qc_stability_studies SET next_pull_date = (
			SELECT min(due_date) FROM qc_stability_pull_points WHERE study_business_id = $1 AND status = 'pending'),
			status = CASE WHEN status = 'planned' THEN 'in_progress' ELSE status END, updated_at = NOW()
		WHERE business_id = $1`, study); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.ListPullPoints(ctx, study)
}

// StabilityShelfLife fits one parameter's results and reads the shelf life
// against the active finished-goods spec (any stage when none), or the limits
// given.
func (s *Store) StabilityShelfLife(ctx context.Context, study, parameter, grade string, lsl, usl *float64) (domain.ShelfLife, string, error) {
	parameter = CanonicalParameter(parameter)
	if parameter == "" {
		return domain.ShelfLife{}, "", ErrBadInput
	}
	results, err := s.stabilityResults(ctx, strings.TrimSpace(study), parameter)
	if err != nil {
		return domain.ShelfLife{}, "", err
	}
	specID := ""
	if lsl == nil && usl == nil {
		spec, err := s.ResolveSpec(ctx, "finished", parameter, grade)
		switch {
		case err == nil:
			lsl, usl, specID = spec.LSL, spec.USL, spec.BusinessID
		case !errors.Is(err, ErrNotFound):
			return domain.ShelfLife{}, "", err
		}
	}
	points := make([]domain.StabilityPoint, len(results))
	for i, r := range results {
		points[i] = domain.StabilityPoint{Day: float64(r.PullDay), Value: r.Value}
	}
	return domain.EstimateShelfLife(points, lsl, usl), specID, nil
}

// DuePull is a pending pull coming due, for the reminder.
type DuePull struct {
	StudyBusinessID string `json:"study_business_id"`
	Product         string `json:"product"`
	PullDay         int    `json:"pull_day"`
	DueDate         string `json:"due_date"`
	Owner           string `json:"owner"`
}

// DueStabilityPulls lists pending pulls due within the next days (or
// overdue) that have not been reminded; unreminded=false lists all of them.
func (s *Store) DueStabilityPulls(ctx context.Context, within int, unreminded bool) ([]DuePull, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT p.study_business_id, st.product, p.pull_day, p.due_date::text, st.owner
		FROM qc_stability_pull_points p JOIN qc_stability_studies st ON st.business_id = p.study_business_id
		WHERE p.status = 'pending' AND p.due_date <= CURRENT_DATE + $1::int
		  AND (NOT $2 OR p.reminded_at IS NULL)
		ORDER BY p.due_date`, within, unreminded)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DuePull{}
	for rows.Next() {
		var d DuePull
		if err := rows.Scan(&d.StudyBusinessID, &d.Product, &d.PullDay, &d.DueDate, &d.Owner); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) MarkPullReminded(ctx context.Context, study string, day int) error {
	_, err := s.pool.Exec(ctx, `UPDATE qc_stability_pull_points SET reminded_at = NOW()
		WHERE study_business_id = $1 AND pull_day = $2`, study, day)
	return err
}
