package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"iag-quality-control/backend/internal/domain"
)

func (s *Store) UpsertBatchLabSummary(ctx context.Context, in UpsertLabSummaryInput) (BatchLabSummary, error) {
	bid := strings.TrimSpace(in.BatchBusinessID)
	if bid == "" {
		return BatchLabSummary{}, ErrBadInput
	}
	grade := strings.TrimSpace(in.Grade)
	if grade == "" && in.CupScore != nil {
		grade = domain.SCATier(*in.CupScore)
	}
	labDate := time.Now().UTC().Format("2006-01-02")

	var out BatchLabSummary
	var labDateOut *string
	err := s.pool.QueryRow(ctx, `
		INSERT INTO qc_batch_lab_summary (
			batch_business_id, moisture, water_activity, cup_score, grade, defects,
			tester, lab_date, latest_sample_id, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,NOW())
		ON CONFLICT (batch_business_id) DO UPDATE SET
			moisture = COALESCE(EXCLUDED.moisture, qc_batch_lab_summary.moisture),
			water_activity = COALESCE(EXCLUDED.water_activity, qc_batch_lab_summary.water_activity),
			cup_score = COALESCE(EXCLUDED.cup_score, qc_batch_lab_summary.cup_score),
			grade = CASE WHEN EXCLUDED.grade <> '' THEN EXCLUDED.grade ELSE qc_batch_lab_summary.grade END,
			defects = COALESCE(EXCLUDED.defects, qc_batch_lab_summary.defects),
			tester = CASE WHEN EXCLUDED.tester <> '' THEN EXCLUDED.tester ELSE qc_batch_lab_summary.tester END,
			lab_date = COALESCE(EXCLUDED.lab_date, qc_batch_lab_summary.lab_date),
			latest_sample_id = CASE WHEN EXCLUDED.latest_sample_id <> '' THEN EXCLUDED.latest_sample_id ELSE qc_batch_lab_summary.latest_sample_id END,
			updated_at = NOW()
		RETURNING batch_business_id, moisture, water_activity, cup_score, grade, defects,
		          tester, lab_date::text, latest_sample_id, updated_at`,
		bid, in.Moisture, in.WaterActivity, in.CupScore, grade, in.Defects,
		strings.TrimSpace(in.Tester), labDate, strings.TrimSpace(in.LatestSampleID),
	).Scan(
		&out.BatchBusinessID, &out.Moisture, &out.WaterActivity, &out.CupScore, &out.Grade, &out.Defects,
		&out.Tester, &labDateOut, &out.LatestSampleID, &out.UpdatedAt,
	)
	if err != nil {
		return BatchLabSummary{}, err
	}
	out.LabDate = labDateOut
	return out, nil
}

func (s *Store) GetBatchLabSummary(ctx context.Context, batchID string) (BatchLabSummary, error) {
	var out BatchLabSummary
	var labDateOut *string
	err := s.pool.QueryRow(ctx, `
		SELECT batch_business_id, moisture, water_activity, cup_score, grade, defects,
		       tester, lab_date::text, latest_sample_id, updated_at
		FROM qc_batch_lab_summary WHERE batch_business_id = $1`, batchID,
	).Scan(
		&out.BatchBusinessID, &out.Moisture, &out.WaterActivity, &out.CupScore, &out.Grade, &out.Defects,
		&out.Tester, &labDateOut, &out.LatestSampleID, &out.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return BatchLabSummary{}, ErrNotFound
	}
	if err != nil {
		return BatchLabSummary{}, err
	}
	out.LabDate = labDateOut
	return out, nil
}

func (s *Store) RecordLabResult(ctx context.Context, in RecordLabResultInput) (BatchLabSummary, error) {
	bid := strings.TrimSpace(in.BatchBusinessID)
	if bid == "" {
		return BatchLabSummary{}, ErrBadInput
	}
	var moisture, cupScore, waterAct *float64
	var defects *int
	if in.Moisture > 0 {
		moisture = &in.Moisture
	}
	if in.CupScore > 0 {
		cupScore = &in.CupScore
	}
	if in.WaterActivity > 0 {
		waterAct = &in.WaterActivity
	}
	if in.Defects > 0 {
		d := int(in.Defects)
		defects = &d
	}
	grade := strings.TrimSpace(in.Grade)
	if grade == "" && cupScore != nil {
		grade = domain.SCATier(*cupScore)
	}
	return s.UpsertBatchLabSummary(ctx, UpsertLabSummaryInput{
		BatchBusinessID: bid,
		Moisture:        moisture,
		WaterActivity:   waterAct,
		CupScore:        cupScore,
		Grade:           grade,
		Defects:         defects,
		Tester:          strings.TrimSpace(in.Tester),
	})
}
