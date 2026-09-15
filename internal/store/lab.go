package store

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

// LabMethod is a written test procedure (migration 010).
type LabMethod struct {
	BusinessID         string         `json:"business_id"`
	Name               string         `json:"name"`
	Version            string         `json:"version"`
	Scope              string         `json:"scope"`
	Equipment          string         `json:"equipment"`
	Procedure          string         `json:"procedure"`
	AcceptanceCriteria string         `json:"acceptance_criteria"`
	Status             string         `json:"status"`
	Attrs              map[string]any `json:"attrs"`
	CreatedAt          time.Time      `json:"created_at"`
	UpdatedAt          time.Time      `json:"updated_at"`
}

// LabRequest is work asked of the lab (migration 010).
type LabRequest struct {
	BusinessID  string         `json:"business_id"`
	RequestDate *string        `json:"request_date,omitempty"`
	Product     string         `json:"product"`
	RequestedBy string         `json:"requested_by"`
	Priority    string         `json:"priority"`
	Objective   string         `json:"objective"`
	NeededBy    *string        `json:"needed_by,omitempty"`
	MethodID    string         `json:"method_id"`
	Status      string         `json:"status"`
	Notes       string         `json:"notes"`
	Attrs       map[string]any `json:"attrs"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

func attrsBytes(m map[string]any) []byte {
	if m == nil {
		return []byte(`{}`)
	}
	b, err := json.Marshal(m)
	if err != nil {
		return []byte(`{}`)
	}
	return b
}

func attrsMap(b []byte) map[string]any {
	out := map[string]any{}
	if len(b) > 0 {
		_ = json.Unmarshal(b, &out)
	}
	return out
}

const labMethodCols = `business_id, name, version, scope, equipment, procedure, acceptance_criteria, status, attrs, created_at, updated_at`

func (s *Store) ListLabMethods(ctx context.Context) ([]LabMethod, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+labMethodCols+` FROM qc_lab_methods ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LabMethod{}
	for rows.Next() {
		var m LabMethod
		var attrs []byte
		if err := rows.Scan(&m.BusinessID, &m.Name, &m.Version, &m.Scope, &m.Equipment, &m.Procedure,
			&m.AcceptanceCriteria, &m.Status, &attrs, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, err
		}
		m.Attrs = attrsMap(attrs)
		out = append(out, m)
	}
	return out, rows.Err()
}

// UpsertLabMethod creates or replaces a method by business_id; a blank id is
// minted. Name is required.
func (s *Store) UpsertLabMethod(ctx context.Context, in LabMethod) (LabMethod, error) {
	id := strings.TrimSpace(in.BusinessID)
	if id == "" {
		var err error
		if id, err = nextBusinessID(ctx, s.pool, "qc_lab_methods", "LM"); err != nil {
			return LabMethod{}, err
		}
	}
	if strings.TrimSpace(in.Name) == "" {
		return LabMethod{}, ErrBadInput
	}
	if in.Status == "" {
		in.Status = "draft"
	}
	if in.Version == "" {
		in.Version = "1"
	}
	var out LabMethod
	var attrs []byte
	err := s.pool.QueryRow(ctx, `
		INSERT INTO qc_lab_methods (business_id, name, version, scope, equipment, procedure, acceptance_criteria, status, attrs, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,NOW())
		ON CONFLICT (business_id) DO UPDATE SET
			name = EXCLUDED.name, version = EXCLUDED.version, scope = EXCLUDED.scope,
			equipment = EXCLUDED.equipment, procedure = EXCLUDED.procedure,
			acceptance_criteria = EXCLUDED.acceptance_criteria, status = EXCLUDED.status,
			attrs = EXCLUDED.attrs, updated_at = NOW()
		RETURNING `+labMethodCols,
		id, strings.TrimSpace(in.Name), in.Version, in.Scope, in.Equipment, in.Procedure,
		in.AcceptanceCriteria, in.Status, attrsBytes(in.Attrs),
	).Scan(&out.BusinessID, &out.Name, &out.Version, &out.Scope, &out.Equipment, &out.Procedure,
		&out.AcceptanceCriteria, &out.Status, &attrs, &out.CreatedAt, &out.UpdatedAt)
	out.Attrs = attrsMap(attrs)
	return out, err
}

const labRequestCols = `business_id, request_date::text, product, requested_by, priority, objective, needed_by::text, method_id, status, notes, attrs, created_at, updated_at`

func (s *Store) ListLabRequests(ctx context.Context, status string) ([]LabRequest, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+labRequestCols+` FROM qc_lab_requests
		WHERE ($1 = '' OR status = $1) ORDER BY needed_by NULLS LAST, updated_at DESC`, strings.TrimSpace(status))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LabRequest{}
	for rows.Next() {
		var r LabRequest
		var attrs []byte
		if err := rows.Scan(&r.BusinessID, &r.RequestDate, &r.Product, &r.RequestedBy, &r.Priority, &r.Objective,
			&r.NeededBy, &r.MethodID, &r.Status, &r.Notes, &attrs, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		r.Attrs = attrsMap(attrs)
		out = append(out, r)
	}
	return out, rows.Err()
}

// UpsertLabRequest creates or replaces a request by business_id. Product is
// required — a request for nothing is not work.
func (s *Store) UpsertLabRequest(ctx context.Context, in LabRequest) (LabRequest, error) {
	id := strings.TrimSpace(in.BusinessID)
	if id == "" {
		var err error
		if id, err = nextBusinessID(ctx, s.pool, "qc_lab_requests", "LR"); err != nil {
			return LabRequest{}, err
		}
	}
	if strings.TrimSpace(in.Product) == "" {
		return LabRequest{}, ErrBadInput
	}
	if in.Status == "" {
		in.Status = "open"
	}
	if in.Priority == "" {
		in.Priority = "normal"
	}
	var out LabRequest
	var attrs []byte
	err := s.pool.QueryRow(ctx, `
		INSERT INTO qc_lab_requests (business_id, request_date, product, requested_by, priority, objective, needed_by, method_id, status, notes, attrs, updated_at)
		VALUES ($1,NULLIF($2,'')::date,$3,$4,$5,$6,NULLIF($7,'')::date,$8,$9,$10,$11,NOW())
		ON CONFLICT (business_id) DO UPDATE SET
			request_date = EXCLUDED.request_date, product = EXCLUDED.product,
			requested_by = EXCLUDED.requested_by, priority = EXCLUDED.priority,
			objective = EXCLUDED.objective, needed_by = EXCLUDED.needed_by,
			method_id = EXCLUDED.method_id, status = EXCLUDED.status, notes = EXCLUDED.notes,
			attrs = EXCLUDED.attrs, updated_at = NOW()
		RETURNING `+labRequestCols,
		id, deref(in.RequestDate), strings.TrimSpace(in.Product), in.RequestedBy, in.Priority, in.Objective,
		deref(in.NeededBy), in.MethodID, in.Status, in.Notes, attrsBytes(in.Attrs),
	).Scan(&out.BusinessID, &out.RequestDate, &out.Product, &out.RequestedBy, &out.Priority, &out.Objective,
		&out.NeededBy, &out.MethodID, &out.Status, &out.Notes, &attrs, &out.CreatedAt, &out.UpdatedAt)
	out.Attrs = attrsMap(attrs)
	return out, err
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
