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

// attrsOptional returns nil when the caller sent no attrs at all, so the SQL
// can leave the stored bag alone instead of replacing it with {}.
// UpsertLabMethodInput is the write shape for a method.
//
// It is separate from LabMethod because the handler used to bind the model
// struct directly, which meant making fields optional would have turned every
// empty string in the JSON *response* into null and broken readers that expect
// a string. Optional fields follow the nil/""/value contract in optional.go.
type UpsertLabMethodInput struct {
	BusinessID         string
	Name               string
	Version            *string
	Scope              *string
	Equipment          *string
	Procedure          *string
	AcceptanceCriteria *string
	Status             *string
	Attrs              map[string]any
}

// UpsertLabRequestInput is the write shape for a request; see
// UpsertLabMethodInput for why it is separate from the model.
type UpsertLabRequestInput struct {
	BusinessID  string
	Product     string
	RequestDate *string
	RequestedBy *string
	Priority    *string
	Objective   *string
	NeededBy    *string
	MethodID    *string
	Status      *string
	Notes       *string
	Attrs       map[string]any
}

func attrsOptional(m map[string]any) []byte {
	if m == nil {
		return nil
	}
	return attrsBytes(m)
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

// UpsertLabMethod creates or updates a method by business_id, preserving any
// column the caller did not send. A blank id is minted. Name is required.
//
// It used to replace every column from EXCLUDED, so a save from a screen that
// carried fewer fields than the table emptied the rest — and replaced the whole
// attrs bag rather than merging into it. Attrs is now merged with ||, because
// the Lab app writes only the keys its own screen knows about and must not wipe
// a key some other writer owns. Removing a key is deliberately not supported:
// set it to "" instead.
func (s *Store) UpsertLabMethod(ctx context.Context, in UpsertLabMethodInput) (LabMethod, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return LabMethod{}, ErrBadInput
	}
	id := strings.TrimSpace(in.BusinessID)
	if id == "" {
		var err error
		if id, err = nextBusinessID(ctx, s.pool, "qc_lab_methods", "LM"); err != nil {
			return LabMethod{}, err
		}
	}
	var out LabMethod
	var attrs []byte
	err := s.pool.QueryRow(ctx, `
		INSERT INTO qc_lab_methods (business_id, name, version, scope, equipment, procedure, acceptance_criteria, status, attrs, updated_at)
		VALUES (
			$1, $2,
			COALESCE($3::text, '1'), COALESCE($4::text, ''), COALESCE($5::text, ''),
			COALESCE($6::text, ''), COALESCE($7::text, ''), COALESCE($8::text, 'draft'),
			COALESCE($9::jsonb, '{}'::jsonb), NOW()
		)
		ON CONFLICT (business_id) DO UPDATE SET
			name = EXCLUDED.name,
			version = COALESCE($3::text, qc_lab_methods.version),
			scope = COALESCE($4::text, qc_lab_methods.scope),
			equipment = COALESCE($5::text, qc_lab_methods.equipment),
			procedure = COALESCE($6::text, qc_lab_methods.procedure),
			acceptance_criteria = COALESCE($7::text, qc_lab_methods.acceptance_criteria),
			status = COALESCE($8::text, qc_lab_methods.status),
			attrs = CASE WHEN $9::jsonb IS NULL THEN qc_lab_methods.attrs
			             ELSE qc_lab_methods.attrs || $9::jsonb END,
			updated_at = NOW()
		RETURNING `+labMethodCols,
		id, name, blankToNil(in.Version), trimOptional(in.Scope), trimOptional(in.Equipment),
		in.Procedure, in.AcceptanceCriteria, blankToNil(in.Status), attrsOptional(in.Attrs),
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

// UpsertLabRequest creates or updates a request by business_id, preserving any
// column the caller did not send. Product is required — a request for nothing
// is not work. See UpsertLabMethod for the attrs-merge rationale.
func (s *Store) UpsertLabRequest(ctx context.Context, in UpsertLabRequestInput) (LabRequest, error) {
	product := strings.TrimSpace(in.Product)
	if product == "" {
		return LabRequest{}, ErrBadInput
	}
	id := strings.TrimSpace(in.BusinessID)
	if id == "" {
		var err error
		if id, err = nextBusinessID(ctx, s.pool, "qc_lab_requests", "LR"); err != nil {
			return LabRequest{}, err
		}
	}
	var out LabRequest
	var attrs []byte
	err := s.pool.QueryRow(ctx, `
		INSERT INTO qc_lab_requests (business_id, request_date, product, requested_by, priority, objective, needed_by, method_id, status, notes, attrs, updated_at)
		VALUES (
			$1, NULLIF($2::text, '')::date, $3,
			COALESCE($4::text, ''), COALESCE($5::text, 'normal'), COALESCE($6::text, ''),
			NULLIF($7::text, '')::date, COALESCE($8::text, ''), COALESCE($9::text, 'open'),
			COALESCE($10::text, ''), COALESCE($11::jsonb, '{}'::jsonb), NOW()
		)
		ON CONFLICT (business_id) DO UPDATE SET
			request_date = CASE WHEN $2::text IS NULL THEN qc_lab_requests.request_date
			                    WHEN $2::text = '' THEN NULL
			                    ELSE $2::date END,
			product = EXCLUDED.product,
			requested_by = COALESCE($4::text, qc_lab_requests.requested_by),
			priority = COALESCE($5::text, qc_lab_requests.priority),
			objective = COALESCE($6::text, qc_lab_requests.objective),
			needed_by = CASE WHEN $7::text IS NULL THEN qc_lab_requests.needed_by
			                 WHEN $7::text = '' THEN NULL
			                 ELSE $7::date END,
			method_id = COALESCE($8::text, qc_lab_requests.method_id),
			status = COALESCE($9::text, qc_lab_requests.status),
			notes = COALESCE($10::text, qc_lab_requests.notes),
			attrs = CASE WHEN $11::jsonb IS NULL THEN qc_lab_requests.attrs
			             ELSE qc_lab_requests.attrs || $11::jsonb END,
			updated_at = NOW()
		RETURNING `+labRequestCols,
		id, in.RequestDate, product, trimOptional(in.RequestedBy), blankToNil(in.Priority),
		in.Objective, in.NeededBy, trimOptional(in.MethodID), blankToNil(in.Status),
		in.Notes, attrsOptional(in.Attrs),
	).Scan(&out.BusinessID, &out.RequestDate, &out.Product, &out.RequestedBy, &out.Priority, &out.Objective,
		&out.NeededBy, &out.MethodID, &out.Status, &out.Notes, &attrs, &out.CreatedAt, &out.UpdatedAt)
	out.Attrs = attrsMap(attrs)
	return out, err
}
