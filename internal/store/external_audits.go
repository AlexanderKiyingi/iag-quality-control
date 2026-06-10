package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type ExternalAudit struct {
	BusinessID  string    `json:"business_id"`
	AuditType   string    `json:"audit_type"`
	Body        string    `json:"body"`
	Description string    `json:"description"`
	AuditDate   string    `json:"audit_date"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
}

type UpsertExternalAuditInput struct {
	BusinessID  string
	AuditType   string
	Body        string
	Description string
	AuditDate   string
	Status      string
}

func (s *Store) ListExternalAudits(ctx context.Context, from, to string) ([]ExternalAudit, error) {
	q := `
		SELECT business_id, audit_type, body, description, audit_date::text, status, created_at
		FROM qc_external_audits WHERE 1=1`
	args := []any{}
	n := 1
	if from != "" {
		q += fmt.Sprintf(" AND audit_date >= $%d::date", n)
		args = append(args, from)
		n++
	}
	if to != "" {
		q += fmt.Sprintf(" AND audit_date <= $%d::date", n)
		args = append(args, to)
	}
	q += ` ORDER BY audit_date ASC`
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ExternalAudit
	for rows.Next() {
		var item ExternalAudit
		if err := rows.Scan(&item.BusinessID, &item.AuditType, &item.Body, &item.Description, &item.AuditDate, &item.Status, &item.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) UpsertExternalAudit(ctx context.Context, in UpsertExternalAuditInput) (ExternalAudit, error) {
	auditType := strings.TrimSpace(in.AuditType)
	date := strings.TrimSpace(in.AuditDate)
	if auditType == "" || date == "" {
		return ExternalAudit{}, ErrBadInput
	}
	id := strings.TrimSpace(in.BusinessID)
	if id == "" {
		var err error
		id, err = nextBusinessID(ctx, s.pool, "qc_external_audits", "AUD")
		if err != nil {
			return ExternalAudit{}, err
		}
	}
	status := strings.TrimSpace(in.Status)
	if status == "" {
		status = "scheduled"
	}
	var out ExternalAudit
	err := s.pool.QueryRow(ctx, `
		INSERT INTO qc_external_audits (business_id, audit_type, body, description, audit_date, status)
		VALUES ($1,$2,$3,$4,$5::date,$6)
		ON CONFLICT (business_id) DO UPDATE SET
			audit_type = EXCLUDED.audit_type,
			body = EXCLUDED.body,
			description = EXCLUDED.description,
			audit_date = EXCLUDED.audit_date,
			status = EXCLUDED.status
		RETURNING business_id, audit_type, body, description, audit_date::text, status, created_at`,
		id, auditType, strings.TrimSpace(in.Body), strings.TrimSpace(in.Description), date, status,
	).Scan(&out.BusinessID, &out.AuditType, &out.Body, &out.Description, &out.AuditDate, &out.Status, &out.CreatedAt)
	return out, err
}

func (s *Store) GetExternalAudit(ctx context.Context, businessID string) (ExternalAudit, error) {
	var out ExternalAudit
	err := s.pool.QueryRow(ctx, `
		SELECT business_id, audit_type, body, description, audit_date::text, status, created_at
		FROM qc_external_audits WHERE business_id = $1`, businessID,
	).Scan(&out.BusinessID, &out.AuditType, &out.Body, &out.Description, &out.AuditDate, &out.Status, &out.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ExternalAudit{}, ErrNotFound
	}
	return out, err
}
