package store

import (
	"context"
	"time"
)

type DayReport struct {
	ReportDate        string  `json:"report_date"`
	SamplesReceived   int     `json:"samples_received"`
	SamplesCompleted  int     `json:"samples_completed"`
	PhysicalTests     int     `json:"physical_tests"`
	ChemicalTests     int     `json:"chemical_tests"`
	CuppingSessions   int     `json:"cupping_sessions"`
	CoAsIssued        int     `json:"coas_issued"`
	CertificationsPending int `json:"certifications_pending"`
	OpenCAPAs         int     `json:"open_capas"`
	OverdueCalibrations int   `json:"overdue_calibrations"`
	AvgCupScore       *float64 `json:"avg_cup_score,omitempty"`
	AvgMoisture       *float64 `json:"avg_moisture,omitempty"`
}

func (s *Store) DayReport(ctx context.Context, date string) (DayReport, error) {
	if date == "" {
		date = time.Now().UTC().Format("2006-01-02")
	}
	var out DayReport
	out.ReportDate = date
	err := s.pool.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*)::int FROM qc_samples WHERE received_at::date = $1::date),
			(SELECT COUNT(*)::int FROM qc_samples WHERE completed_at::date = $1::date),
			(SELECT COUNT(*)::int FROM qc_physical_tests WHERE tested_at::date = $1::date),
			(SELECT COUNT(*)::int FROM qc_chemical_tests WHERE tested_at::date = $1::date),
			(SELECT COUNT(*)::int FROM qc_cupping_sessions WHERE session_date = $1::date),
			(SELECT COUNT(*)::int FROM qc_coa WHERE issued_at::date = $1::date),
			(SELECT COUNT(*)::int FROM qc_certification_requests WHERE status = 'pending'),
			(SELECT COUNT(*)::int FROM qc_capas WHERE status <> 'closed'),
			(SELECT COUNT(*)::int FROM qc_instruments WHERE next_cal_date IS NOT NULL AND next_cal_date < CURRENT_DATE),
			(SELECT AVG(total_score) FROM qc_cupping_sessions WHERE session_date = $1::date),
			(SELECT AVG(moisture_pct) FROM qc_physical_tests WHERE tested_at::date = $1::date)
	`, date).Scan(
		&out.SamplesReceived, &out.SamplesCompleted, &out.PhysicalTests, &out.ChemicalTests,
		&out.CuppingSessions, &out.CoAsIssued, &out.CertificationsPending, &out.OpenCAPAs,
		&out.OverdueCalibrations, &out.AvgCupScore, &out.AvgMoisture,
	)
	return out, err
}

type TrendPoint struct {
	Date            string   `json:"date"`
	SamplesReceived int      `json:"samples_received"`
	CuppingSessions int      `json:"cupping_sessions"`
	AvgCupScore     *float64 `json:"avg_cup_score,omitempty"`
	AvgMoisture     *float64 `json:"avg_moisture,omitempty"`
	Deviations      int      `json:"deviations"`
}

type WeeklySummary struct {
	Days              []DayReport `json:"days"`
	TotalSamples      int         `json:"total_samples"`
	TotalCupping      int         `json:"total_cupping"`
	TotalCoAs         int         `json:"total_coas"`
	AvgCupScore       *float64    `json:"avg_cup_score,omitempty"`
	TotalDeviations   int         `json:"total_deviations"`
}

func (s *Store) Trends(ctx context.Context, days int) ([]TrendPoint, error) {
	if days <= 0 || days > 90 {
		days = 7
	}
	rows, err := s.pool.Query(ctx, `
		WITH dates AS (
			SELECT generate_series(CURRENT_DATE - ($1::int - 1), CURRENT_DATE, '1 day'::interval)::date AS d
		)
		SELECT d::text,
			(SELECT COUNT(*)::int FROM qc_samples WHERE received_at::date = d),
			(SELECT COUNT(*)::int FROM qc_cupping_sessions WHERE session_date = d),
			(SELECT AVG(total_score) FROM qc_cupping_sessions WHERE session_date = d),
			(SELECT AVG(moisture_pct) FROM qc_physical_tests WHERE tested_at::date = d),
			(SELECT COUNT(*)::int FROM qc_compliance_logs WHERE logged_date = d AND status = 'deviation')
		FROM dates
		ORDER BY d ASC`, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TrendPoint
	for rows.Next() {
		var p TrendPoint
		if err := rows.Scan(&p.Date, &p.SamplesReceived, &p.CuppingSessions, &p.AvgCupScore, &p.AvgMoisture, &p.Deviations); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) WeeklySummary(ctx context.Context) (WeeklySummary, error) {
	var out WeeklySummary
	for i := 6; i >= 0; i-- {
		d := time.Now().UTC().AddDate(0, 0, -i).Format("2006-01-02")
		day, err := s.DayReport(ctx, d)
		if err != nil {
			return out, err
		}
		out.Days = append(out.Days, day)
		out.TotalSamples += day.SamplesReceived
		out.TotalCupping += day.CuppingSessions
		out.TotalCoAs += day.CoAsIssued
	}
	rows, err := s.pool.Query(ctx, `
		SELECT AVG(total_score)
		FROM qc_cupping_sessions
		WHERE session_date >= CURRENT_DATE - INTERVAL '6 days'`)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	if rows.Next() {
		_ = rows.Scan(&out.AvgCupScore)
	}
	_ = s.pool.QueryRow(ctx, `
		SELECT COUNT(*)::int FROM qc_compliance_logs
		WHERE logged_date >= CURRENT_DATE - INTERVAL '6 days' AND status = 'deviation'
	`).Scan(&out.TotalDeviations)
	return out, nil
}
