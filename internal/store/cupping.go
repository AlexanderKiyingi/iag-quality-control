package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"iag-quality-control/backend/internal/domain"
)

func (s *Store) CreateCupping(ctx context.Context, in CreateCuppingInput) (CuppingSession, error) {
	sample, err := s.GetSample(ctx, in.SampleBusinessID)
	if err != nil {
		return CuppingSession{}, err
	}
	businessID, err := nextBusinessID(ctx, s.pool, "qc_cupping_sessions", "CUP")
	if err != nil {
		return CuppingSession{}, err
	}
	scores := map[string]float64{
		"fragrance":  in.Fragrance,
		"flavor":     in.Flavor,
		"aftertaste": in.Aftertaste,
		"acidity":    in.Acidity,
		"body":       in.Body,
		"balance":    in.Balance,
		"uniformity": in.Uniformity,
		"cleancup":   in.CleanCup,
		"sweetness":  in.Sweetness,
		"overall":    in.Overall,
	}
	total := domain.CalcSCATotal(scores, in.DefectCat1, in.DefectCat2)
	grade := domain.SCATier(total)
	scorers := in.Scorers
	if scorers == nil {
		scorers = []string{}
	}
	scorersJSON, err := json.Marshal(scorers)
	if err != nil {
		return CuppingSession{}, err
	}
	sessionDate := time.Now().UTC().Format("2006-01-02")

	var out CuppingSession
	err = s.pool.QueryRow(ctx, `
		INSERT INTO qc_cupping_sessions (
			business_id, sample_business_id, batch_business_id, session_date, scorers,
			fragrance, flavor, aftertaste, acidity, body, balance, uniformity, cleancup, sweetness, overall,
			defect_cat1, defect_cat2, total_score, notes
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)
		RETURNING business_id, sample_business_id, batch_business_id, session_date::text, scorers,
		          fragrance, flavor, aftertaste, acidity, body, balance, uniformity, cleancup, sweetness, overall,
		          defect_cat1, defect_cat2, total_score, notes, status`,
		businessID, sample.BusinessID, sample.BatchBusinessID, sessionDate, scorersJSON,
		in.Fragrance, in.Flavor, in.Aftertaste, in.Acidity, in.Body, in.Balance,
		in.Uniformity, in.CleanCup, in.Sweetness, in.Overall,
		in.DefectCat1, in.DefectCat2, total, strings.TrimSpace(in.Notes),
	).Scan(
		&out.BusinessID, &out.SampleBusinessID, &out.BatchBusinessID, &out.SessionDate, &scorersJSON,
		&out.Fragrance, &out.Flavor, &out.Aftertaste, &out.Acidity, &out.Body, &out.Balance,
		&out.Uniformity, &out.CleanCup, &out.Sweetness, &out.Overall,
		&out.DefectCat1, &out.DefectCat2, &out.TotalScore, &out.Notes, &out.Status,
	)
	if err != nil {
		return CuppingSession{}, fmt.Errorf("create cupping: %w", err)
	}
	_ = json.Unmarshal(scorersJSON, &out.Scorers)
	out.Grade = grade

	defects := in.DefectCat2
	if _, err := s.UpsertBatchLabSummary(ctx, UpsertLabSummaryInput{
		BatchBusinessID: sample.BatchBusinessID,
		CupScore:        &total,
		Grade:           grade,
		Defects:         &defects,
		Tester:          strings.Join(scorers, ", "),
		LatestSampleID:  sample.BusinessID,
	}); err != nil {
		return CuppingSession{}, err
	}
	if _, err := s.UpdateSampleStatus(ctx, sample.BusinessID, "complete"); err != nil {
		return CuppingSession{}, err
	}
	return out, nil
}

func (s *Store) GetLatestCuppingBySample(ctx context.Context, sampleID string) (CuppingSession, error) {
	var out CuppingSession
	var scorersJSON []byte
	err := s.pool.QueryRow(ctx, `
		SELECT business_id, sample_business_id, batch_business_id, session_date::text, scorers,
		       fragrance, flavor, aftertaste, acidity, body, balance, uniformity, cleancup, sweetness, overall,
		       defect_cat1, defect_cat2, total_score, notes, status
		FROM qc_cupping_sessions
		WHERE sample_business_id = $1
		ORDER BY created_at DESC LIMIT 1`, sampleID,
	).Scan(
		&out.BusinessID, &out.SampleBusinessID, &out.BatchBusinessID, &out.SessionDate, &scorersJSON,
		&out.Fragrance, &out.Flavor, &out.Aftertaste, &out.Acidity, &out.Body, &out.Balance,
		&out.Uniformity, &out.CleanCup, &out.Sweetness, &out.Overall,
		&out.DefectCat1, &out.DefectCat2, &out.TotalScore, &out.Notes, &out.Status,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return CuppingSession{}, ErrNotFound
		}
		return CuppingSession{}, err
	}
	_ = json.Unmarshal(scorersJSON, &out.Scorers)
	out.Grade = domain.SCATier(out.TotalScore)
	return out, nil
}

func (s *Store) ListCuppingSessions(ctx context.Context, batchID, sampleID string, limit int) ([]CuppingSession, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := `
		SELECT business_id, sample_business_id, batch_business_id, session_date::text, scorers,
		       fragrance, flavor, aftertaste, acidity, body, balance, uniformity, cleancup, sweetness, overall,
		       defect_cat1, defect_cat2, total_score, notes, status
		FROM qc_cupping_sessions WHERE 1=1`
	args := []any{}
	n := 1
	if batchID != "" {
		q += fmt.Sprintf(" AND batch_business_id = $%d", n)
		args = append(args, batchID)
		n++
	}
	if sampleID != "" {
		q += fmt.Sprintf(" AND sample_business_id = $%d", n)
		args = append(args, sampleID)
		n++
	}
	q += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d", n)
	args = append(args, limit)
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanCuppingRows(rows)
}

func (s *Store) ListCuppingBySample(ctx context.Context, sampleID string, limit int) ([]CuppingSession, error) {
	return s.ListCuppingSessions(ctx, "", sampleID, limit)
}

func (s *Store) ListCuppingByBatch(ctx context.Context, batchID string, limit int) ([]CuppingSession, error) {
	return s.ListCuppingSessions(ctx, batchID, "", limit)
}

func scanCuppingRows(rows rowScanner) ([]CuppingSession, error) {
	var out []CuppingSession
	for rows.Next() {
		var item CuppingSession
		var scorersJSON []byte
		if err := rows.Scan(
			&item.BusinessID, &item.SampleBusinessID, &item.BatchBusinessID, &item.SessionDate, &scorersJSON,
			&item.Fragrance, &item.Flavor, &item.Aftertaste, &item.Acidity, &item.Body, &item.Balance,
			&item.Uniformity, &item.CleanCup, &item.Sweetness, &item.Overall,
			&item.DefectCat1, &item.DefectCat2, &item.TotalScore, &item.Notes, &item.Status,
		); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(scorersJSON, &item.Scorers)
		item.Grade = domain.SCATier(item.TotalScore)
		out = append(out, item)
	}
	return out, rows.Err()
}
