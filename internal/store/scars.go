package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// SCAR is a supplier corrective-action request (migration 023).
type SCAR struct {
	BusinessID         string         `json:"business_id"`
	SupplierRef        string         `json:"supplier_ref"`
	NCRef              string         `json:"nc_ref"`
	InspectionRef      string         `json:"inspection_ref"`
	Title              string         `json:"title"`
	Description        string         `json:"description"`
	RequestedAction    string         `json:"requested_action"`
	Severity           string         `json:"severity"`
	DueDate            *string        `json:"due_date,omitempty"`
	Status             string         `json:"status"`
	SupplierContact    string         `json:"supplier_contact"`
	SupplierRootCause  string         `json:"supplier_root_cause"`
	SupplierAction     string         `json:"supplier_action"`
	ResponseReceivedAt *time.Time     `json:"response_received_at,omitempty"`
	EffectivenessCheck string         `json:"effectiveness_check"`
	Effective          *bool          `json:"effective,omitempty"`
	ClosedAt           *time.Time     `json:"closed_at,omitempty"`
	Overdue            bool           `json:"overdue"`
	Attrs              map[string]any `json:"attrs"`
	UpdatedBy          string         `json:"updated_by"`
	ChangeReason       string         `json:"change_reason"`
	CreatedAt          time.Time      `json:"created_at"`
	UpdatedAt          time.Time      `json:"updated_at"`
}

// SCARInput creates or edits a SCAR; nil leaves a field alone.
type SCARInput struct {
	SupplierRef        *string        `json:"supplier_ref"`
	NCRef              *string        `json:"nc_ref"`
	InspectionRef      *string        `json:"inspection_ref"`
	Title              *string        `json:"title"`
	Description        *string        `json:"description"`
	RequestedAction    *string        `json:"requested_action"`
	Severity           *string        `json:"severity"`
	DueDate            *string        `json:"due_date"`
	Status             *string        `json:"status"`
	SupplierContact    *string        `json:"supplier_contact"`
	SupplierRootCause  *string        `json:"supplier_root_cause"`
	SupplierAction     *string        `json:"supplier_action"`
	EffectivenessCheck *string        `json:"effectiveness_check"`
	Effective          *bool          `json:"effective"`
	Attrs              map[string]any `json:"attrs"`
	ChangeReason       string         `json:"change_reason"`
	Actor              string         `json:"-"`
}

const scarCols = `business_id, supplier_ref, nc_ref, inspection_ref, title, description, requested_action,
	severity, due_date::text, status, supplier_contact, supplier_root_cause, supplier_action,
	response_received_at, effectiveness_check, effective, closed_at, attrs, updated_by, change_reason,
	created_at, updated_at,
	(status NOT IN ('closed', 'cancelled') AND due_date IS NOT NULL AND due_date < CURRENT_DATE)`

func scanSCAR(row interface{ Scan(...any) error }) (SCAR, error) {
	var s SCAR
	var attrs []byte
	err := row.Scan(&s.BusinessID, &s.SupplierRef, &s.NCRef, &s.InspectionRef, &s.Title, &s.Description,
		&s.RequestedAction, &s.Severity, &s.DueDate, &s.Status, &s.SupplierContact, &s.SupplierRootCause,
		&s.SupplierAction, &s.ResponseReceivedAt, &s.EffectivenessCheck, &s.Effective, &s.ClosedAt, &attrs,
		&s.UpdatedBy, &s.ChangeReason, &s.CreatedAt, &s.UpdatedAt, &s.Overdue)
	if err != nil {
		return SCAR{}, err
	}
	s.Attrs = map[string]any{}
	_ = json.Unmarshal(attrs, &s.Attrs)
	return s, nil
}

// scarStatuses a plain save may set. 'closed' is a signature (POST
// /signatures, entity scars), never a field.
var scarStatuses = map[string]bool{"open": true, "awaiting_response": true, "response_received": true,
	"verifying": true, "cancelled": true}

/*
supplierForNC finds the supplier behind a non-conformance: an NC raised by an
incoming inspection names it (auto_source or source_ref), and that inspection
names its supplier.
*/
func (s *Store) supplierForNC(ctx context.Context, ncRef string) (supplier, inspection string, err error) {
	err = s.pool.QueryRow(ctx, `
		SELECT i.supplier_ref, i.business_id FROM qc_non_conformances nc
		JOIN qc_incoming_inspections i ON i.business_id IN (nc.auto_source, nc.source_ref)
		WHERE nc.business_id = $1 AND i.supplier_ref <> ''
		LIMIT 1`, strings.TrimSpace(ncRef)).Scan(&supplier, &inspection)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", nil
	}
	return supplier, inspection, err
}

// CreateSCAR raises a SCAR. Raised from an NC on an inspected lot, it takes
// the supplier from that lot when none is given.
func (s *Store) CreateSCAR(ctx context.Context, in SCARInput) (SCAR, error) {
	val := func(p *string) string {
		if p == nil {
			return ""
		}
		return strings.TrimSpace(*p)
	}
	supplier, inspection := val(in.SupplierRef), val(in.InspectionRef)
	if nc := val(in.NCRef); nc != "" {
		if _, err := s.GetNonConformance(ctx, nc); err != nil {
			return SCAR{}, err
		}
		sup, insp, err := s.supplierForNC(ctx, nc)
		if err != nil {
			return SCAR{}, err
		}
		// The NC's lot names its supplier; a SCAR that names another one is
		// addressed to the wrong company.
		if supplier != "" && sup != "" && supplier != sup {
			return SCAR{}, fmt.Errorf("%w: %s came from a lot supplied by %s, not %s", ErrBadInput, nc, sup, supplier)
		}
		if supplier == "" {
			supplier = sup
		}
		if inspection == "" {
			inspection = insp
		}
	}
	if supplier == "" {
		return SCAR{}, fmt.Errorf("%w: a SCAR names its supplier (or comes from an NC on an inspected lot)", ErrBadInput)
	}
	if val(in.Title) == "" {
		return SCAR{}, fmt.Errorf("%w: a SCAR needs a title", ErrBadInput)
	}
	severity := val(in.Severity)
	if severity == "" {
		severity = "major"
	}
	id, err := nextBusinessID(ctx, s.pool, "qc_scars", "SCAR")
	if err != nil {
		return SCAR{}, err
	}
	attrs, _ := json.Marshal(nonNilAttrs(in.Attrs))
	row := s.pool.QueryRow(ctx, `
		INSERT INTO qc_scars (business_id, supplier_ref, nc_ref, inspection_ref, title, description,
			requested_action, severity, due_date, status, supplier_contact, attrs, updated_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, COALESCE(NULLIF($9, '')::date, CURRENT_DATE + 30), 'open', $10, $11::jsonb, $12)
		RETURNING `+scarCols,
		id, supplier, val(in.NCRef), inspection, val(in.Title), val(in.Description), val(in.RequestedAction),
		severity, val(in.DueDate), val(in.SupplierContact), attrs, strings.TrimSpace(in.Actor))
	out, err := scanSCAR(row)
	if err != nil && strings.Contains(err.Error(), "severity") {
		return SCAR{}, fmt.Errorf("%w: severity is minor, major or critical", ErrBadInput)
	}
	return out, err
}

func (s *Store) GetSCAR(ctx context.Context, id string) (SCAR, error) {
	out, err := scanSCAR(s.pool.QueryRow(ctx, `SELECT `+scarCols+` FROM qc_scars WHERE business_id = $1`, strings.TrimSpace(id)))
	if errors.Is(err, pgx.ErrNoRows) {
		return SCAR{}, ErrNotFound
	}
	return out, err
}

func (s *Store) ListSCARs(ctx context.Context, supplier, status string, limit int) ([]SCAR, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.pool.Query(ctx, `SELECT `+scarCols+` FROM qc_scars
		WHERE ($1 = '' OR supplier_ref = $1)
		  AND ($2 = '' OR status = $2 OR ($2 = 'active' AND status NOT IN ('closed', 'cancelled')))
		ORDER BY created_at DESC LIMIT $3`, strings.TrimSpace(supplier), strings.TrimSpace(status), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SCAR{}
	for rows.Next() {
		sc, err := scanSCAR(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sc)
	}
	return out, rows.Err()
}

/*
UpdateSCAR edits a SCAR: the supplier's response, the effectiveness check, the
status short of closed. A closed SCAR is locked (019) and every edit says why.
Recording the supplier's root cause or action stamps when the response came.
*/
func (s *Store) UpdateSCAR(ctx context.Context, id string, in SCARInput) (SCAR, error) {
	id = strings.TrimSpace(id)
	if err := s.guardWrite(ctx, "scars", "qc_scars", "business_id", id, in.ChangeReason); err != nil {
		return SCAR{}, err
	}
	if _, err := s.GetSCAR(ctx, id); err != nil {
		return SCAR{}, err
	}
	if in.Status != nil {
		st := strings.TrimSpace(*in.Status)
		if st == "closed" {
			return SCAR{}, ErrSignatureState
		}
		if !scarStatuses[st] {
			return SCAR{}, fmt.Errorf("%w: status is open, awaiting_response, response_received, verifying or cancelled", ErrBadInput)
		}
	}
	responded := (in.SupplierRootCause != nil && strings.TrimSpace(*in.SupplierRootCause) != "") ||
		(in.SupplierAction != nil && strings.TrimSpace(*in.SupplierAction) != "")
	attrs := attrsOptional(in.Attrs)
	row := s.pool.QueryRow(ctx, `
		UPDATE qc_scars SET
			title = COALESCE($2, title),
			description = COALESCE($3, description),
			requested_action = COALESCE($4, requested_action),
			severity = COALESCE($5, severity),
			due_date = CASE WHEN $6::text IS NULL THEN due_date WHEN $6 = '' THEN NULL ELSE $6::date END,
			status = COALESCE($7, CASE WHEN $13 AND status IN ('open', 'awaiting_response') THEN 'response_received' ELSE status END),
			supplier_contact = COALESCE($8, supplier_contact),
			supplier_root_cause = COALESCE($9, supplier_root_cause),
			supplier_action = COALESCE($10, supplier_action),
			response_received_at = CASE WHEN $13 THEN COALESCE(response_received_at, NOW()) ELSE response_received_at END,
			effectiveness_check = COALESCE($11, effectiveness_check),
			effective = COALESCE($12, effective),
			attrs = CASE WHEN $14::jsonb IS NULL THEN attrs ELSE attrs || $14::jsonb END,
			updated_by = $15, change_reason = $16, updated_at = NOW()
		WHERE business_id = $1
		RETURNING `+scarCols,
		id, trimOptional(in.Title), trimOptional(in.Description), trimOptional(in.RequestedAction),
		trimOptional(in.Severity), trimOptional(in.DueDate), trimOptional(in.Status), trimOptional(in.SupplierContact),
		trimOptional(in.SupplierRootCause), trimOptional(in.SupplierAction), trimOptional(in.EffectivenessCheck),
		in.Effective, responded, attrs, strings.TrimSpace(in.Actor), strings.TrimSpace(in.ChangeReason))
	out, err := scanSCAR(row)
	if err != nil && strings.Contains(err.Error(), "severity") {
		return SCAR{}, fmt.Errorf("%w: severity is minor, major or critical", ErrBadInput)
	}
	return out, err
}

func nonNilAttrs(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}
