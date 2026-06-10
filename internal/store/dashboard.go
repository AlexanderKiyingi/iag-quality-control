package store

import (
	"context"
)

type DashboardSummary struct {
	SamplesTotal        int      `json:"samples_total"`
	SamplesToday        int      `json:"samples_today"`
	SamplesPending      int      `json:"samples_pending"`
	SamplesInProgress   int      `json:"samples_in_progress"`
	SamplesComplete     int      `json:"samples_complete"`
	SamplesRetest       int      `json:"samples_retest"`
	AvgCupScore         *float64 `json:"avg_cup_score,omitempty"`
	AvgMoisture         *float64 `json:"avg_moisture,omitempty"`
	AvgDefectPoints7d   *float64 `json:"avg_defect_points_7d,omitempty"`
	CertificationsMTD   int      `json:"certifications_mtd"`
	CoAsIssuedMTD       int      `json:"coas_issued_mtd"`
	OpenCAPAs           int      `json:"open_capas"`
	ActiveCoAs          int      `json:"active_coas"`
	BatchesTested       int      `json:"batches_tested"`
}

func (s *Store) DashboardSummary(ctx context.Context) (DashboardSummary, error) {
	var out DashboardSummary
	err := s.pool.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*)::int FROM qc_samples),
			(SELECT COUNT(*)::int FROM qc_samples WHERE received_at::date = CURRENT_DATE),
			(SELECT COUNT(*)::int FROM qc_samples WHERE status IN ('pending','urgent')),
			(SELECT COUNT(*)::int FROM qc_samples WHERE status = 'in-progress'),
			(SELECT COUNT(*)::int FROM qc_samples WHERE status = 'complete'),
			(SELECT COUNT(*)::int FROM qc_samples WHERE status = 'retest'),
			(SELECT AVG(cup_score) FROM qc_batch_lab_summary WHERE cup_score IS NOT NULL),
			(SELECT AVG(moisture) FROM qc_batch_lab_summary WHERE moisture IS NOT NULL),
			(SELECT AVG((defect_cat1 * 4 + defect_cat2)::float8)
			 FROM qc_physical_tests
			 WHERE tested_at >= NOW() - INTERVAL '7 days'),
			(SELECT COUNT(*)::int FROM qc_certification_requests
			 WHERE stage = 'export_ready'
			   AND date_trunc('month', COALESCE(completed_at, requested_at)) = date_trunc('month', CURRENT_DATE)),
			(SELECT COUNT(*)::int FROM qc_coa
			 WHERE date_trunc('month', issued_at) = date_trunc('month', CURRENT_DATE)),
			(SELECT COUNT(*)::int FROM qc_capas WHERE status <> 'closed'),
			(SELECT COUNT(*)::int FROM qc_coa WHERE status = 'active'),
			(SELECT COUNT(*)::int FROM qc_batch_lab_summary)
	`).Scan(
		&out.SamplesTotal, &out.SamplesToday, &out.SamplesPending, &out.SamplesInProgress, &out.SamplesComplete,
		&out.SamplesRetest, &out.AvgCupScore, &out.AvgMoisture, &out.AvgDefectPoints7d,
		&out.CertificationsMTD, &out.CoAsIssuedMTD, &out.OpenCAPAs, &out.ActiveCoAs, &out.BatchesTested,
	)
	return out, err
}
