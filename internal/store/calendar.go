package store

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type CalendarEvent struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Title  string `json:"title"`
	Date   string `json:"date"`
	Status string `json:"status"`
	Detail string `json:"detail"`
	RefID  string `json:"ref_id"`
}

func (s *Store) Calendar(ctx context.Context, from, to string) ([]CalendarEvent, error) {
	fromDate, toDate, err := parseCalendarRange(from, to)
	if err != nil {
		return nil, err
	}
	var out []CalendarEvent

	certRows, err := s.pool.Query(ctx, `
		SELECT business_id, batch_business_id, lot_business_id, stage, status, requested_at::date::text
		FROM qc_certification_requests
		WHERE status = 'pending'
		  AND stage <> 'export_ready'
		  AND requested_at::date BETWEEN $1::date AND $2::date
		ORDER BY requested_at ASC`, fromDate, toDate)
	if err != nil {
		return nil, err
	}
	defer certRows.Close()
	for certRows.Next() {
		var id, batch, lot, stage, status, date string
		if err := certRows.Scan(&id, &batch, &lot, &stage, &status, &date); err != nil {
			return nil, err
		}
		title := "Certification review"
		if lot != "" {
			title = fmt.Sprintf("Certify %s", lot)
		}
		out = append(out, CalendarEvent{
			ID: "cert-" + id, Kind: "certification", Title: title, Date: date,
			Status: status, Detail: fmt.Sprintf("Stage %s · batch %s", stage, batch), RefID: id,
		})
	}

	calRows, err := s.pool.Query(ctx, `
		SELECT business_id, name, next_cal_date::text,
		       CASE WHEN next_cal_date < CURRENT_DATE THEN 'overdue' ELSE 'due' END
		FROM qc_instruments
		WHERE next_cal_date IS NOT NULL
		  AND next_cal_date BETWEEN $1::date AND $2::date
		ORDER BY next_cal_date ASC`, fromDate, toDate)
	if err != nil {
		return nil, err
	}
	defer calRows.Close()
	for calRows.Next() {
		var id, name, date, status string
		if err := calRows.Scan(&id, &name, &date, &status); err != nil {
			return nil, err
		}
		out = append(out, CalendarEvent{
			ID: "cal-" + id, Kind: "calibration", Title: name + " calibration",
			Date: date, Status: status, Detail: "Instrument calibration due", RefID: id,
		})
	}

	auditRows, err := s.pool.Query(ctx, `
		SELECT business_id, log_type, ccp, logged_date::text, status
		FROM qc_compliance_logs
		WHERE log_type ILIKE '%audit%'
		  AND logged_date BETWEEN $1::date AND $2::date
		ORDER BY logged_date ASC`, fromDate, toDate)
	if err != nil {
		return nil, err
	}
	defer auditRows.Close()
	for auditRows.Next() {
		var id, logType, ccp, date, status string
		if err := auditRows.Scan(&id, &logType, &ccp, &date, &status); err != nil {
			return nil, err
		}
		out = append(out, CalendarEvent{
			ID: "audit-" + id, Kind: "compliance", Title: logType + " audit",
			Date: date, Status: status, Detail: ccp, RefID: id,
		})
	}
	extRows, err := s.pool.Query(ctx, `
		SELECT business_id, audit_type, body, description, audit_date::text, status
		FROM qc_external_audits
		WHERE audit_date BETWEEN $1::date AND $2::date
		ORDER BY audit_date ASC`, fromDate, toDate)
	if err != nil {
		return nil, err
	}
	defer extRows.Close()
	for extRows.Next() {
		var id, auditType, body, desc, date, status string
		if err := extRows.Scan(&id, &auditType, &body, &desc, &date, &status); err != nil {
			return nil, err
		}
		out = append(out, CalendarEvent{
			ID: "ext-" + id, Kind: "external_audit", Title: auditType+" audit",
			Date: date, Status: status, Detail: body + " · " + desc, RefID: id,
		})
	}

	return out, nil
}

func parseCalendarRange(from, to string) (string, string, error) {
	now := time.Now().UTC()
	if strings.TrimSpace(from) == "" {
		from = now.Format("2006-01-02")
	}
	if strings.TrimSpace(to) == "" {
		to = now.AddDate(0, 1, 0).Format("2006-01-02")
	}
	if _, err := time.Parse("2006-01-02", from); err != nil {
		return "", "", ErrBadInput
	}
	if _, err := time.Parse("2006-01-02", to); err != nil {
		return "", "", ErrBadInput
	}
	return from, to, nil
}
