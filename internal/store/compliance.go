package store

import (
	"context"
	"strings"
	"time"
)

type ComplianceLog struct {
	BusinessID    string    `json:"business_id"`
	LogType       string    `json:"log_type"`
	CCP           string    `json:"ccp"`
	LoggedDate    string    `json:"logged_date"`
	LoggedTime    string    `json:"logged_time"`
	TechID        string    `json:"tech_id"`
	MeasuredValue string    `json:"measured_value"`
	LimitValue    string    `json:"limit_value"`
	Status        string    `json:"status"`
	ActionTaken   string    `json:"action_taken"`
	CreatedAt     time.Time `json:"created_at"`
}

type CAPA struct {
	BusinessID       string  `json:"business_id"`
	Title            string  `json:"title"`
	SourceRef        string  `json:"source_ref"`
	Status           string  `json:"status"`
	Priority         string  `json:"priority"`
	Owner            string  `json:"owner"`
	RootCause        string  `json:"root_cause"`
	CorrectiveAction string  `json:"corrective_action"`
	OpenedAt         string  `json:"opened_at"`
	ClosedAt         *string `json:"closed_at,omitempty"`
}

type CreateComplianceLogInput struct {
	LogType       string
	CCP           string
	LoggedDate    string
	LoggedTime    string
	TechID        string
	MeasuredValue string
	LimitValue    string
	Status        string
	ActionTaken   string
}

type UpsertCAPAInput struct {
	BusinessID       string
	Title            string
	SourceRef        string
	Status           string
	Priority         string
	Owner            string
	RootCause        string
	CorrectiveAction string
	OpenedAt         string
	ClosedAt         *string
}

func (s *Store) ListComplianceLogs(ctx context.Context, limit int) ([]ComplianceLog, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
		SELECT business_id, log_type, ccp, logged_date::text, logged_time, tech_id,
		       measured_value, limit_value, status, action_taken, created_at
		FROM qc_compliance_logs ORDER BY logged_date DESC, created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ComplianceLog
	for rows.Next() {
		var item ComplianceLog
		if err := rows.Scan(
			&item.BusinessID, &item.LogType, &item.CCP, &item.LoggedDate, &item.LoggedTime, &item.TechID,
			&item.MeasuredValue, &item.LimitValue, &item.Status, &item.ActionTaken, &item.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) CreateComplianceLog(ctx context.Context, in CreateComplianceLogInput) (ComplianceLog, error) {
	id, err := nextBusinessID(ctx, s.pool, "qc_compliance_logs", "CPL")
	if err != nil {
		return ComplianceLog{}, err
	}
	status := strings.TrimSpace(in.Status)
	if status == "" {
		status = "pass"
	}
	loggedDate := strings.TrimSpace(in.LoggedDate)
	if loggedDate == "" {
		loggedDate = time.Now().UTC().Format("2006-01-02")
	}
	var out ComplianceLog
	err = s.pool.QueryRow(ctx, `
		INSERT INTO qc_compliance_logs (
			business_id, log_type, ccp, logged_date, logged_time, tech_id,
			measured_value, limit_value, status, action_taken
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		RETURNING business_id, log_type, ccp, logged_date::text, logged_time, tech_id,
		          measured_value, limit_value, status, action_taken, created_at`,
		id, strings.TrimSpace(in.LogType), strings.TrimSpace(in.CCP), loggedDate,
		strings.TrimSpace(in.LoggedTime), strings.TrimSpace(in.TechID),
		strings.TrimSpace(in.MeasuredValue), strings.TrimSpace(in.LimitValue), status,
		strings.TrimSpace(in.ActionTaken),
	).Scan(
		&out.BusinessID, &out.LogType, &out.CCP, &out.LoggedDate, &out.LoggedTime, &out.TechID,
		&out.MeasuredValue, &out.LimitValue, &out.Status, &out.ActionTaken, &out.CreatedAt,
	)
	return out, err
}

func (s *Store) ListCAPAs(ctx context.Context, status string, limit int) ([]CAPA, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	q := `
		SELECT business_id, title, source_ref, status, priority, owner,
		       root_cause, corrective_action, opened_at::text, closed_at::text
		FROM qc_capas`
	args := []any{}
	if status != "" && status != "all" {
		q += " WHERE status = $1"
		args = append(args, status)
		q += " ORDER BY opened_at DESC LIMIT $2"
		args = append(args, limit)
	} else {
		q += " ORDER BY opened_at DESC LIMIT $1"
		args = append(args, limit)
	}
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CAPA
	for rows.Next() {
		var item CAPA
		if err := rows.Scan(
			&item.BusinessID, &item.Title, &item.SourceRef, &item.Status, &item.Priority, &item.Owner,
			&item.RootCause, &item.CorrectiveAction, &item.OpenedAt, &item.ClosedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) UpsertCAPA(ctx context.Context, in UpsertCAPAInput) (CAPA, error) {
	id := strings.TrimSpace(in.BusinessID)
	if id == "" {
		var err error
		id, err = nextBusinessID(ctx, s.pool, "qc_capas", "CAPA")
		if err != nil {
			return CAPA{}, err
		}
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return CAPA{}, ErrBadInput
	}
	status := strings.TrimSpace(in.Status)
	if status == "" {
		status = "open"
	}
	openedAt := strings.TrimSpace(in.OpenedAt)
	if openedAt == "" {
		openedAt = time.Now().UTC().Format("2006-01-02")
	}
	var out CAPA
	err := s.pool.QueryRow(ctx, `
		INSERT INTO qc_capas (
			business_id, title, source_ref, status, priority, owner,
			root_cause, corrective_action, opened_at, closed_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT (business_id) DO UPDATE SET
			title = EXCLUDED.title,
			source_ref = EXCLUDED.source_ref,
			status = EXCLUDED.status,
			priority = EXCLUDED.priority,
			owner = EXCLUDED.owner,
			root_cause = EXCLUDED.root_cause,
			corrective_action = EXCLUDED.corrective_action,
			opened_at = EXCLUDED.opened_at,
			closed_at = EXCLUDED.closed_at
		RETURNING business_id, title, source_ref, status, priority, owner,
		          root_cause, corrective_action, opened_at::text, closed_at::text`,
		id, title, strings.TrimSpace(in.SourceRef), status, strings.TrimSpace(in.Priority),
		strings.TrimSpace(in.Owner), strings.TrimSpace(in.RootCause), strings.TrimSpace(in.CorrectiveAction),
		openedAt, in.ClosedAt,
	).Scan(
		&out.BusinessID, &out.Title, &out.SourceRef, &out.Status, &out.Priority, &out.Owner,
		&out.RootCause, &out.CorrectiveAction, &out.OpenedAt, &out.ClosedAt,
	)
	return out, err
}

func (s *Store) OpenCAPAsCount(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*)::int FROM qc_capas WHERE status <> 'closed'`).Scan(&n)
	return n, err
}

func (s *Store) OverdueInstrumentsCount(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*)::int FROM qc_instruments
		WHERE next_cal_date IS NOT NULL AND next_cal_date < CURRENT_DATE`).Scan(&n)
	return n, err
}
