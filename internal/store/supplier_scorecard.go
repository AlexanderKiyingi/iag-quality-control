package store

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"iag-quality-control/backend/internal/domain"
)

// MonthTrend is one month of a supplier's lots.
type MonthTrend struct {
	Month          string   `json:"month"`
	Lots           int      `json:"lots"`
	Accepted       int      `json:"accepted"`
	AcceptanceRate *float64 `json:"acceptance_rate,omitempty"`
}

// SupplierScorecard is quality's view of one supplier over a window.
type SupplierScorecard struct {
	SupplierRef     string       `json:"supplier_ref"`
	Days            int          `json:"days"`
	Lots            int          `json:"lots"`
	LotsDecided     int          `json:"lots_decided"`
	LotsAccepted    int          `json:"lots_accepted"`
	LotsRejected    int          `json:"lots_rejected"`
	AcceptanceRate  *float64     `json:"acceptance_rate,omitempty"`
	AvgMoisture     *float64     `json:"avg_moisture,omitempty"`
	AvgDefects      *float64     `json:"avg_defects,omitempty"`
	NonConformances int          `json:"non_conformances"`
	OpenSCARs       int          `json:"open_scars"`
	OverdueSCARs    int          `json:"overdue_scars"`
	CriticalSCARs   int          `json:"critical_scars"`
	LastInspected   *string      `json:"last_inspected,omitempty"`
	Recommendation  string       `json:"recommended_status"`
	Reasons         []string     `json:"reasons"`
	Trend           []MonthTrend `json:"trend"`
}

/*
An inspected lot is decided when it was accepted or rejected — by the spec
verdict where there was one, else by the inspector's result. A lot still open
or with no result yet is counted but not decided.
*/
const lotDecision = `CASE
	WHEN i.verdict = 'pass' THEN 'accepted'
	WHEN i.verdict = 'fail' THEN 'rejected'
	WHEN lower(i.result) IN ('accept', 'accepted', 'pass', 'release', 'released') THEN 'accepted'
	WHEN lower(i.result) IN ('reject', 'rejected', 'fail', 'quarantine', 'return') THEN 'rejected'
	ELSE '' END`

// SupplierScorecards rolls up every supplier that has inspections or SCARs,
// or the one named.
func (s *Store) SupplierScorecards(ctx context.Context, supplier string, days int) ([]SupplierScorecard, error) {
	if days <= 0 || days > 1095 {
		days = 365
	}
	supplier = strings.TrimSpace(supplier)
	rows, err := s.pool.Query(ctx, `
		WITH suppliers AS (
			SELECT supplier_ref FROM qc_incoming_inspections WHERE supplier_ref <> ''
			UNION SELECT supplier_ref FROM qc_scars
		),
		lots AS (
			SELECT i.supplier_ref, i.business_id, i.inspected_at, i.moisture_pct, i.defect_count, `+lotDecision+` AS decision
			FROM qc_incoming_inspections i
			WHERE i.supplier_ref <> '' AND COALESCE(i.inspected_at, i.created_at::date) >= CURRENT_DATE - $1::int
		)
		SELECT s.supplier_ref,
			(SELECT COUNT(*) FROM lots l WHERE l.supplier_ref = s.supplier_ref)::int,
			(SELECT COUNT(*) FROM lots l WHERE l.supplier_ref = s.supplier_ref AND l.decision <> '')::int,
			(SELECT COUNT(*) FROM lots l WHERE l.supplier_ref = s.supplier_ref AND l.decision = 'accepted')::int,
			(SELECT round(avg(moisture_pct)::numeric, 2)::float8 FROM lots l WHERE l.supplier_ref = s.supplier_ref),
			(SELECT round(avg(defect_count)::numeric, 2)::float8 FROM lots l WHERE l.supplier_ref = s.supplier_ref),
			(SELECT COUNT(*) FROM qc_non_conformances nc JOIN lots l ON l.business_id IN (nc.auto_source, nc.source_ref)
			  WHERE l.supplier_ref = s.supplier_ref)::int,
			(SELECT COUNT(*) FROM qc_scars sc WHERE sc.supplier_ref = s.supplier_ref AND sc.status NOT IN ('closed','cancelled'))::int,
			(SELECT COUNT(*) FROM qc_scars sc WHERE sc.supplier_ref = s.supplier_ref AND sc.status NOT IN ('closed','cancelled')
			  AND sc.due_date < CURRENT_DATE)::int,
			(SELECT COUNT(*) FROM qc_scars sc WHERE sc.supplier_ref = s.supplier_ref AND sc.status NOT IN ('closed','cancelled')
			  AND sc.severity = 'critical')::int,
			(SELECT max(inspected_at)::text FROM lots l WHERE l.supplier_ref = s.supplier_ref)
		FROM suppliers s
		WHERE $2 = '' OR s.supplier_ref = $2
		ORDER BY s.supplier_ref`, days, supplier)
	if err != nil {
		return nil, err
	}
	out := []SupplierScorecard{}
	for rows.Next() {
		c := SupplierScorecard{Days: days, Trend: []MonthTrend{}}
		if err := rows.Scan(&c.SupplierRef, &c.Lots, &c.LotsDecided, &c.LotsAccepted, &c.AvgMoisture, &c.AvgDefects,
			&c.NonConformances, &c.OpenSCARs, &c.OverdueSCARs, &c.CriticalSCARs, &c.LastInspected); err != nil {
			rows.Close()
			return nil, err
		}
		c.LotsRejected = c.LotsDecided - c.LotsAccepted
		c.Recommendation, c.Reasons, c.AcceptanceRate = domain.RecommendSupplierStatus(domain.SupplierEvidence{
			LotsDecided: c.LotsDecided, LotsAccepted: c.LotsAccepted,
			OverdueSCARs: c.OverdueSCARs, CriticalSCARs: c.CriticalSCARs,
		})
		out = append(out, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		trend, err := s.supplierTrend(ctx, out[i].SupplierRef, days)
		if err != nil {
			return nil, err
		}
		out[i].Trend = trend
	}
	return out, nil
}

func (s *Store) supplierTrend(ctx context.Context, supplier string, days int) ([]MonthTrend, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT to_char(date_trunc('month', COALESCE(i.inspected_at, i.created_at::date)), 'YYYY-MM'),
		       COUNT(*) FILTER (WHERE (`+lotDecision+`) <> '')::int,
		       COUNT(*) FILTER (WHERE (`+lotDecision+`) = 'accepted')::int
		FROM qc_incoming_inspections i
		WHERE i.supplier_ref = $1 AND COALESCE(i.inspected_at, i.created_at::date) >= CURRENT_DATE - $2::int
		GROUP BY 1 ORDER BY 1`, supplier, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MonthTrend{}
	for rows.Next() {
		var m MonthTrend
		if err := rows.Scan(&m.Month, &m.Lots, &m.Accepted); err != nil {
			return nil, err
		}
		if m.Lots > 0 {
			r := round2(float64(m.Accepted) / float64(m.Lots) * 100)
			m.AcceptanceRate = &r
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

/*
RecordSupplierRecommendation remembers the recommendation QC last announced
for a supplier and reports whether it changed — so qc.supplier.scorecard is
published when the advice changes, not on every inspection.
*/
func (s *Store) RecordSupplierRecommendation(ctx context.Context, c SupplierScorecard) (bool, error) {
	reasons, _ := json.Marshal(c.Reasons)
	var prev string
	err := s.pool.QueryRow(ctx, `SELECT recommended_status FROM qc_supplier_quality WHERE supplier_ref = $1`, c.SupplierRef).Scan(&prev)
	if err != nil && !strings.Contains(err.Error(), "no rows") {
		return false, err
	}
	if prev == c.Recommendation {
		return false, nil
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO qc_supplier_quality (supplier_ref, recommended_status, acceptance_rate, lots, reasons, published_at)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6)
		ON CONFLICT (supplier_ref) DO UPDATE SET recommended_status = EXCLUDED.recommended_status,
			acceptance_rate = EXCLUDED.acceptance_rate, lots = EXCLUDED.lots, reasons = EXCLUDED.reasons,
			published_at = EXCLUDED.published_at`,
		c.SupplierRef, c.Recommendation, c.AcceptanceRate, c.LotsDecided, reasons, time.Now().UTC())
	return err == nil, err
}
