package store

import (
	"context"
	"time"
)

// DayPoint is one day of the dashboard's trend lines.
type DayPoint struct {
	Day             string   `json:"day"`
	SamplesReceived int      `json:"samples_received"`
	Results         int      `json:"results"`
	Judged          int      `json:"judged"`
	Failed          int      `json:"failed"`
	FailureRate     *float64 `json:"failure_rate,omitempty"`
	CupScore        *float64 `json:"cup_score,omitempty"`
	CupperSpread    *float64 `json:"cupper_spread,omitempty"`
}

type CountBy struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}

type OverdueCAPA struct {
	BusinessID string `json:"business_id"`
	Title      string `json:"title"`
	Owner      string `json:"owner"`
	DueDate    string `json:"due_date"`
	DaysLate   int    `json:"days_late"`
}

type ActiveHold struct {
	BatchRef string `json:"batch_ref"`
	Since    string `json:"since"`
	Reason   string `json:"reason"`
}

// QualityDashboard is everything the Quality dashboard draws, for one window.
type QualityDashboard struct {
	Days              int           `json:"days"`
	GeneratedAt       time.Time     `json:"generated_at"`
	Daily             []DayPoint    `json:"daily"`
	IncomingVerdicts  []CountBy     `json:"incoming_verdicts"`
	OpenNCsBySeverity []CountBy     `json:"open_ncs_by_severity"`
	NCsRaised         int           `json:"ncs_raised"`
	CAPAsByStatus     []CountBy     `json:"capas_by_status"`
	OverdueCAPAs      []OverdueCAPA `json:"overdue_capas"`
	ActiveHolds       []ActiveHold  `json:"active_holds"`
	OpenSPCAlerts     int           `json:"open_spc_alerts"`
	GradesInWindow    []CountBy     `json:"green_grades"`
}

/*
QualityDashboard reads the trends and the open work for the last days days.

Failure rate is over judged records only — measurements, incoming
inspections, in-process checks and green gradings whose verdict is pass or
fail; a reading with no spec was not judged and does not dilute the rate.
Cupper spread is the mean, per day, of each session's standard deviation of
sheet totals across its cuppers (sessions with two or more sheets).
*/
func (s *Store) QualityDashboard(ctx context.Context, days int) (QualityDashboard, error) {
	if days <= 0 || days > 365 {
		days = 30
	}
	out := QualityDashboard{Days: days, GeneratedAt: time.Now().UTC(), Daily: []DayPoint{}}
	rows, err := s.pool.Query(ctx, `
		WITH d AS (
			SELECT generate_series(CURRENT_DATE - ($1::int - 1), CURRENT_DATE, '1 day')::date AS day
		),
		verdicts AS (
			SELECT created_at::date AS day, verdict FROM qc_lab_measurements WHERE status <> 'void'
			UNION ALL SELECT created_at::date, verdict FROM qc_incoming_inspections
			UNION ALL SELECT created_at::date, verdict FROM qc_in_process_checks
			UNION ALL SELECT graded_at::date, verdict FROM qc_green_gradings WHERE status <> 'void'
		),
		results AS (
			SELECT created_at::date AS day FROM qc_lab_measurements WHERE status <> 'void'
			UNION ALL SELECT graded_at::date FROM qc_green_gradings WHERE status <> 'void'
			UNION ALL SELECT tested_at::date FROM qc_physical_tests
			UNION ALL SELECT tested_at::date FROM qc_chemical_tests
			UNION ALL SELECT created_at::date FROM qc_cupping_sessions
		),
		spread AS (
			SELECT c.created_at::date AS day, stddev_samp(sc.total_score) AS sd
			FROM qc_cupping_sessions c JOIN qc_cupping_scores sc ON sc.session_business_id = c.business_id
			GROUP BY c.business_id, c.created_at::date
			HAVING COUNT(*) >= 2
		)
		SELECT d.day::text,
			(SELECT COUNT(*) FROM qc_samples WHERE received_at::date = d.day)::int,
			(SELECT COUNT(*) FROM results r WHERE r.day = d.day)::int,
			(SELECT COUNT(*) FROM verdicts v WHERE v.day = d.day AND v.verdict IN ('pass','fail'))::int,
			(SELECT COUNT(*) FROM verdicts v WHERE v.day = d.day AND v.verdict = 'fail')::int,
			(SELECT round(avg(total_score)::numeric, 2)::float8 FROM qc_cupping_sessions WHERE created_at::date = d.day),
			(SELECT round(avg(sd)::numeric, 2)::float8 FROM spread WHERE spread.day = d.day)
		FROM d ORDER BY d.day`, days)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var p DayPoint
		if err := rows.Scan(&p.Day, &p.SamplesReceived, &p.Results, &p.Judged, &p.Failed, &p.CupScore, &p.CupperSpread); err != nil {
			rows.Close()
			return out, err
		}
		if p.Judged > 0 {
			r := round2(float64(p.Failed) / float64(p.Judged) * 100)
			p.FailureRate = &r
		}
		out.Daily = append(out.Daily, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return out, err
	}

	countBy := func(q string, args ...any) ([]CountBy, error) {
		rows, err := s.pool.Query(ctx, q, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		list := []CountBy{}
		for rows.Next() {
			var c CountBy
			if err := rows.Scan(&c.Key, &c.Count); err != nil {
				return nil, err
			}
			list = append(list, c)
		}
		return list, rows.Err()
	}
	if out.IncomingVerdicts, err = countBy(`
		SELECT COALESCE(NULLIF(verdict, ''), 'not judged'), COUNT(*)::int FROM qc_incoming_inspections
		WHERE created_at >= CURRENT_DATE - ($1::int - 1) GROUP BY 1 ORDER BY 2 DESC`, days); err != nil {
		return out, err
	}
	if out.OpenNCsBySeverity, err = countBy(`
		SELECT COALESCE(NULLIF(severity, ''), 'unrated'), COUNT(*)::int FROM qc_non_conformances
		WHERE status <> 'closed' GROUP BY 1 ORDER BY 2 DESC`); err != nil {
		return out, err
	}
	if out.CAPAsByStatus, err = countBy(`
		SELECT COALESCE(NULLIF(status, ''), 'open'), COUNT(*)::int FROM qc_capas GROUP BY 1 ORDER BY 2 DESC`); err != nil {
		return out, err
	}
	if out.GradesInWindow, err = countBy(`
		SELECT sca_grade, COUNT(*)::int FROM qc_green_gradings
		WHERE status <> 'void' AND graded_at >= CURRENT_DATE - ($1::int - 1) GROUP BY 1 ORDER BY 2 DESC`, days); err != nil {
		return out, err
	}
	if err := s.pool.QueryRow(ctx, `
		SELECT (SELECT COUNT(*) FROM qc_non_conformances WHERE created_at >= CURRENT_DATE - ($1::int - 1))::int,
		       (SELECT COUNT(*) FROM qc_spc_alerts WHERE status = 'open')::int`, days).
		Scan(&out.NCsRaised, &out.OpenSPCAlerts); err != nil {
		return out, err
	}

	out.OverdueCAPAs = []OverdueCAPA{}
	rows, err = s.pool.Query(ctx, `
		SELECT business_id, title, owner, due_date::text, (CURRENT_DATE - due_date)::int
		FROM qc_capas WHERE status <> 'closed' AND due_date IS NOT NULL AND due_date < CURRENT_DATE
		ORDER BY due_date LIMIT 20`)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var c OverdueCAPA
		if err := rows.Scan(&c.BusinessID, &c.Title, &c.Owner, &c.DueDate, &c.DaysLate); err != nil {
			rows.Close()
			return out, err
		}
		out.OverdueCAPAs = append(out.OverdueCAPAs, c)
	}
	rows.Close()

	// A batch is on hold while its latest event is a hold.
	out.ActiveHolds = []ActiveHold{}
	rows, err = s.pool.Query(ctx, `
		SELECT batch_ref, COALESCE(event_date::text, created_at::date::text), reason FROM (
			SELECT DISTINCT ON (batch_ref) batch_ref, event, event_date, created_at, reason
			FROM qc_hold_events WHERE batch_ref <> ''
			ORDER BY batch_ref, event_date DESC NULLS LAST, created_at DESC
		) latest WHERE event = 'placed_on_hold'
		ORDER BY 2 DESC LIMIT 50`)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var h ActiveHold
		if err := rows.Scan(&h.BatchRef, &h.Since, &h.Reason); err != nil {
			return out, err
		}
		out.ActiveHolds = append(out.ActiveHolds, h)
	}
	return out, rows.Err()
}
