package store

import (
	"context"
	"fmt"
	"strings"
)

func (s *Store) CreatePhysicalTest(ctx context.Context, in CreatePhysicalTestInput) (PhysicalTest, error) {
	sample, err := s.GetSample(ctx, in.SampleBusinessID)
	if err != nil {
		return PhysicalTest{}, err
	}
	businessID, err := nextBusinessID(ctx, s.pool, "qc_physical_tests", "PHY")
	if err != nil {
		return PhysicalTest{}, err
	}
	status := strings.TrimSpace(in.Status)
	if status == "" {
		status = "pass"
	}
	var out PhysicalTest
	err = s.pool.QueryRow(ctx, `
		INSERT INTO qc_physical_tests (
			business_id, sample_business_id, batch_business_id, tech_id,
			moisture_pct, screen_18, screen_17, screen_16, screen_15, screen_pan,
			bulk_density_g_l, defect_cat1, defect_cat2, status
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		RETURNING business_id, sample_business_id, batch_business_id, tech_id,
		          moisture_pct, screen_18, screen_17, screen_16, screen_15, screen_pan,
		          bulk_density_g_l, defect_cat1, defect_cat2, status, tested_at`,
		businessID, sample.BusinessID, sample.BatchBusinessID, strings.TrimSpace(in.TechID),
		in.MoisturePct, in.Screen18, in.Screen17, in.Screen16, in.Screen15, in.ScreenPan,
		in.BulkDensityGL, in.DefectCat1, in.DefectCat2, status,
	).Scan(
		&out.BusinessID, &out.SampleBusinessID, &out.BatchBusinessID, &out.TechID,
		&out.MoisturePct, &out.Screen18, &out.Screen17, &out.Screen16, &out.Screen15, &out.ScreenPan,
		&out.BulkDensityGL, &out.DefectCat1, &out.DefectCat2, &out.Status, &out.TestedAt,
	)
	if err != nil {
		return PhysicalTest{}, fmt.Errorf("create physical test: %w", err)
	}
	if _, err := s.UpdateSampleStatus(ctx, sample.BusinessID, "in-progress"); err != nil {
		return PhysicalTest{}, err
	}
	return out, nil
}

func (s *Store) CreateChemicalTest(ctx context.Context, in CreateChemicalTestInput) (ChemicalTest, error) {
	sample, err := s.GetSample(ctx, in.SampleBusinessID)
	if err != nil {
		return ChemicalTest{}, err
	}
	businessID, err := nextBusinessID(ctx, s.pool, "qc_chemical_tests", "CHM")
	if err != nil {
		return ChemicalTest{}, err
	}
	status := strings.TrimSpace(in.Status)
	if status == "" {
		status = "pass"
	}
	var out ChemicalTest
	err = s.pool.QueryRow(ctx, `
		INSERT INTO qc_chemical_tests (
			business_id, sample_business_id, batch_business_id, tech_id,
			moisture_kf, water_activity, ph, chlorogenic_pct, caffeine_pct, status
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		RETURNING business_id, sample_business_id, batch_business_id, tech_id,
		          moisture_kf, water_activity, ph, chlorogenic_pct, caffeine_pct, status, tested_at`,
		businessID, sample.BusinessID, sample.BatchBusinessID, strings.TrimSpace(in.TechID),
		in.MoistureKF, in.WaterActivity, in.PH, in.ChlorogenicPct, in.CaffeinePct, status,
	).Scan(
		&out.BusinessID, &out.SampleBusinessID, &out.BatchBusinessID, &out.TechID,
		&out.MoistureKF, &out.WaterActivity, &out.PH, &out.ChlorogenicPct, &out.CaffeinePct, &out.Status, &out.TestedAt,
	)
	if err != nil {
		return ChemicalTest{}, fmt.Errorf("create chemical test: %w", err)
	}
	if _, err := s.UpdateSampleStatus(ctx, sample.BusinessID, "in-progress"); err != nil {
		return ChemicalTest{}, err
	}
	return out, nil
}

func (s *Store) ListPhysicalTests(ctx context.Context, batchID, sampleID, status string, limit int) ([]PhysicalTest, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := `
		SELECT business_id, sample_business_id, batch_business_id, tech_id,
		       moisture_pct, screen_18, screen_17, screen_16, screen_15, screen_pan,
		       bulk_density_g_l, defect_cat1, defect_cat2, status, tested_at
		FROM qc_physical_tests WHERE 1=1`
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
	if status != "" && status != "all" {
		q += fmt.Sprintf(" AND status = $%d", n)
		args = append(args, status)
		n++
	}
	q += fmt.Sprintf(" ORDER BY tested_at DESC LIMIT $%d", n)
	args = append(args, limit)
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPhysicalRows(rows)
}

func (s *Store) ListPhysicalTestsBySample(ctx context.Context, sampleID string, limit int) ([]PhysicalTest, error) {
	return s.ListPhysicalTests(ctx, "", sampleID, "", limit)
}

func (s *Store) ListPhysicalTestsByBatch(ctx context.Context, batchID string, limit int) ([]PhysicalTest, error) {
	return s.ListPhysicalTests(ctx, batchID, "", "", limit)
}

func (s *Store) ListChemicalTests(ctx context.Context, batchID, sampleID, status string, limit int) ([]ChemicalTest, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := `
		SELECT business_id, sample_business_id, batch_business_id, tech_id,
		       moisture_kf, water_activity, ph, chlorogenic_pct, caffeine_pct, status, tested_at
		FROM qc_chemical_tests WHERE 1=1`
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
	if status != "" && status != "all" {
		q += fmt.Sprintf(" AND status = $%d", n)
		args = append(args, status)
		n++
	}
	q += fmt.Sprintf(" ORDER BY tested_at DESC LIMIT $%d", n)
	args = append(args, limit)
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanChemicalRows(rows)
}

func (s *Store) ListChemicalTestsBySample(ctx context.Context, sampleID string, limit int) ([]ChemicalTest, error) {
	return s.ListChemicalTests(ctx, "", sampleID, "", limit)
}

func (s *Store) ListChemicalTestsByBatch(ctx context.Context, batchID string, limit int) ([]ChemicalTest, error) {
	return s.ListChemicalTests(ctx, batchID, "", "", limit)
}

type rowScanner interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

func scanPhysicalRows(rows rowScanner) ([]PhysicalTest, error) {
	var out []PhysicalTest
	for rows.Next() {
		var item PhysicalTest
		if err := rows.Scan(
			&item.BusinessID, &item.SampleBusinessID, &item.BatchBusinessID, &item.TechID,
			&item.MoisturePct, &item.Screen18, &item.Screen17, &item.Screen16, &item.Screen15, &item.ScreenPan,
			&item.BulkDensityGL, &item.DefectCat1, &item.DefectCat2, &item.Status, &item.TestedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func scanChemicalRows(rows rowScanner) ([]ChemicalTest, error) {
	var out []ChemicalTest
	for rows.Next() {
		var item ChemicalTest
		if err := rows.Scan(
			&item.BusinessID, &item.SampleBusinessID, &item.BatchBusinessID, &item.TechID,
			&item.MoistureKF, &item.WaterActivity, &item.PH, &item.ChlorogenicPct, &item.CaffeinePct, &item.Status, &item.TestedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
