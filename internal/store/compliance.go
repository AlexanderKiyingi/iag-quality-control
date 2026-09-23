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
	// Migration 012. The CAPA form has collected all four since it was written
	// and silently dropped them for want of a column.
	DueDate       *string        `json:"due_date,omitempty"`
	CAPAKind      string         `json:"capa_kind"`
	Effectiveness string         `json:"effectiveness"`
	Attachments   string         `json:"attachments"`
	Attrs         map[string]any `json:"attrs"`
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

// UpsertCAPAInput is the write shape for a CAPA. Everything but Title follows
// the nil/""/value contract in optional.go.
type UpsertCAPAInput struct {
	BusinessID       string
	Title            string
	SourceRef        *string
	Status           *string
	Priority         *string
	Owner            *string
	RootCause        *string
	CorrectiveAction *string
	OpenedAt         *string
	ClosedAt         *string
	DueDate          *string
	CAPAKind         *string
	Effectiveness    *string
	Attachments      *string
	Attrs            map[string]any
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

const capaCols = `business_id, title, source_ref, status, priority, owner, root_cause,
	corrective_action, opened_at::text, closed_at::text, due_date::text, capa_kind,
	effectiveness, attachments, attrs`

func scanCAPA(rows interface{ Scan(...any) error }) (CAPA, error) {
	var item CAPA
	var attrs []byte
	if err := rows.Scan(&item.BusinessID, &item.Title, &item.SourceRef, &item.Status,
		&item.Priority, &item.Owner, &item.RootCause, &item.CorrectiveAction, &item.OpenedAt,
		&item.ClosedAt, &item.DueDate, &item.CAPAKind, &item.Effectiveness, &item.Attachments,
		&attrs); err != nil {
		return CAPA{}, err
	}
	item.Attrs = attrsMap(attrs)
	return item, nil
}

func (s *Store) ListCAPAs(ctx context.Context, status string, limit int) ([]CAPA, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	q := `
		SELECT ` + capaCols + `
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
	out := []CAPA{}
	for rows.Next() {
		item, err := scanCAPA(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// UpsertCAPA creates or updates a CAPA by business_id, preserving any column
// the caller did not send.
//
// It used to replace every column from EXCLUDED, with status defaulting to
// "open" whenever it was absent — so re-saving a closed CAPA to correct a typo
// in the owner reopened it.
func (s *Store) UpsertCAPA(ctx context.Context, in UpsertCAPAInput) (CAPA, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return CAPA{}, ErrBadInput
	}
	id := strings.TrimSpace(in.BusinessID)
	if id == "" {
		var err error
		id, err = nextBusinessID(ctx, s.pool, "qc_capas", "CAPA")
		if err != nil {
			return CAPA{}, err
		}
	}
	// Only a CAPA being created needs today's date; an update leaves whatever
	// it was opened on alone.
	openedAt := blankToNil(in.OpenedAt)
	rows, err := s.pool.Query(ctx, `
		INSERT INTO qc_capas (
			business_id, title, source_ref, status, priority, owner,
			root_cause, corrective_action, opened_at, closed_at,
			due_date, capa_kind, effectiveness, attachments, attrs
		) VALUES (
			$1, $2,
			COALESCE($3::text, ''), COALESCE($4::text, 'open'), COALESCE($5::text, ''),
			COALESCE($6::text, ''), COALESCE($7::text, ''), COALESCE($8::text, ''),
			COALESCE(NULLIF($9::text, '')::date, CURRENT_DATE), NULLIF($10::text, '')::date,
			NULLIF($11::text, '')::date, COALESCE($12::text, ''), COALESCE($13::text, ''),
			COALESCE($14::text, ''), COALESCE($15::jsonb, '{}'::jsonb)
		)
		ON CONFLICT (business_id) DO UPDATE SET
			title = EXCLUDED.title,
			source_ref = COALESCE($3::text, qc_capas.source_ref),
			status = COALESCE($4::text, qc_capas.status),
			priority = COALESCE($5::text, qc_capas.priority),
			owner = COALESCE($6::text, qc_capas.owner),
			root_cause = COALESCE($7::text, qc_capas.root_cause),
			corrective_action = COALESCE($8::text, qc_capas.corrective_action),
			opened_at = CASE WHEN $9::text IS NULL THEN qc_capas.opened_at
			                 WHEN $9::text = '' THEN NULL
			                 ELSE $9::date END,
			closed_at = CASE WHEN $10::text IS NULL THEN qc_capas.closed_at
			                 WHEN $10::text = '' THEN NULL
			                 ELSE $10::date END,
			due_date = CASE WHEN $11::text IS NULL THEN qc_capas.due_date
			                WHEN $11::text = '' THEN NULL
			                ELSE $11::date END,
			capa_kind = COALESCE($12::text, qc_capas.capa_kind),
			effectiveness = COALESCE($13::text, qc_capas.effectiveness),
			attachments = COALESCE($14::text, qc_capas.attachments),
			attrs = CASE WHEN $15::jsonb IS NULL THEN qc_capas.attrs
			             ELSE qc_capas.attrs || $15::jsonb END
		RETURNING `+capaCols,
		id, title, trimOptional(in.SourceRef), blankToNil(in.Status), trimOptional(in.Priority),
		trimOptional(in.Owner), trimOptional(in.RootCause), trimOptional(in.CorrectiveAction),
		openedAt, in.ClosedAt, in.DueDate, trimOptional(in.CAPAKind),
		in.Effectiveness, in.Attachments, attrsOptional(in.Attrs),
	)
	if err != nil {
		return CAPA{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return CAPA{}, err
		}
		return CAPA{}, ErrNotFound
	}
	return scanCAPA(rows)
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
