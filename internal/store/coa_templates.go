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

// CoAField is one line a template prints.
type CoAField struct {
	Parameter string `json:"parameter"`
	Label     string `json:"label"`
	Unit      string `json:"unit"`
	ShowSpec  bool   `json:"show_spec"`
}

// CoATemplate is a certificate layout (migration 026).
type CoATemplate struct {
	BusinessID      string     `json:"business_id"`
	Name            string     `json:"name"`
	CustomerRef     string     `json:"customer_ref"`
	IsDefault       bool       `json:"is_default"`
	Title           string     `json:"title"`
	HeaderNote      string     `json:"header_note"`
	FooterStatement string     `json:"footer_statement"`
	Parameters      []CoAField `json:"parameters"`
	ShowSignatures  bool       `json:"show_signatures"`
	Active          bool       `json:"active"`
	UpdatedBy       string     `json:"updated_by"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// CoATemplateInput creates or replaces a template.
type CoATemplateInput struct {
	BusinessID      string     `json:"business_id"`
	Name            string     `json:"name"`
	CustomerRef     string     `json:"customer_ref"`
	IsDefault       bool       `json:"is_default"`
	Title           string     `json:"title"`
	HeaderNote      string     `json:"header_note"`
	FooterStatement string     `json:"footer_statement"`
	Parameters      []CoAField `json:"parameters"`
	ShowSignatures  *bool      `json:"show_signatures"`
	Active          *bool      `json:"active"`
	ChangeReason    string     `json:"change_reason"`
	Actor           string     `json:"-"`
}

const coaTemplateCols = `business_id, name, customer_ref, is_default, title, header_note, footer_statement, parameters,
	show_signatures, active, updated_by, updated_at`

func scanCoATemplate(row interface{ Scan(...any) error }) (CoATemplate, error) {
	var t CoATemplate
	var params []byte
	if err := row.Scan(&t.BusinessID, &t.Name, &t.CustomerRef, &t.IsDefault, &t.Title, &t.HeaderNote, &t.FooterStatement,
		&params, &t.ShowSignatures, &t.Active, &t.UpdatedBy, &t.UpdatedAt); err != nil {
		return CoATemplate{}, err
	}
	t.Parameters = []CoAField{}
	_ = json.Unmarshal(params, &t.Parameters)
	return t, nil
}

/*
PutCoATemplate creates a template, or replaces one (with a reason). A
template lists at least one result; parameters are stored by canonical name
so they find the readings however they were typed.
*/
func (s *Store) PutCoATemplate(ctx context.Context, in CoATemplateInput) (CoATemplate, error) {
	if strings.TrimSpace(in.Name) == "" {
		return CoATemplate{}, fmt.Errorf("%w: a template has a name", ErrBadInput)
	}
	fields := []CoAField{}
	for _, f := range in.Parameters {
		p := CanonicalParameter(f.Parameter)
		if p == "" {
			continue
		}
		label := strings.TrimSpace(f.Label)
		if label == "" {
			label = f.Parameter
		}
		fields = append(fields, CoAField{Parameter: p, Label: label, Unit: strings.TrimSpace(f.Unit), ShowSpec: f.ShowSpec})
	}
	if len(fields) == 0 {
		return CoATemplate{}, fmt.Errorf("%w: a template prints at least one result", ErrBadInput)
	}
	id := strings.TrimSpace(in.BusinessID)
	exists := false
	if id != "" {
		if _, err := s.GetCoATemplate(ctx, id); err == nil {
			exists = true
		} else if !errors.Is(err, ErrNotFound) {
			return CoATemplate{}, err
		}
	}
	if exists && strings.TrimSpace(in.ChangeReason) == "" {
		return CoATemplate{}, ErrReasonRequired
	}
	if id == "" {
		var err error
		if id, err = nextBusinessID(ctx, s.pool, "qc_coa_templates", "CTPL"); err != nil {
			return CoATemplate{}, err
		}
	}
	params, _ := json.Marshal(fields)
	show, active := true, true
	if in.ShowSignatures != nil {
		show = *in.ShowSignatures
	}
	if in.Active != nil {
		active = *in.Active
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = "Certificate of Analysis"
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO qc_coa_templates (business_id, name, customer_ref, is_default, title, header_note, footer_statement,
			parameters, show_signatures, active, updated_by, change_reason)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9,$10,$11,$12)
		ON CONFLICT (business_id) DO UPDATE SET name = EXCLUDED.name, customer_ref = EXCLUDED.customer_ref,
			is_default = EXCLUDED.is_default, title = EXCLUDED.title, header_note = EXCLUDED.header_note,
			footer_statement = EXCLUDED.footer_statement, parameters = EXCLUDED.parameters,
			show_signatures = EXCLUDED.show_signatures, active = EXCLUDED.active, updated_by = EXCLUDED.updated_by,
			change_reason = EXCLUDED.change_reason, updated_at = NOW()
		RETURNING `+coaTemplateCols,
		id, strings.TrimSpace(in.Name), strings.TrimSpace(in.CustomerRef), in.IsDefault, title, strings.TrimSpace(in.HeaderNote),
		strings.TrimSpace(in.FooterStatement), params, show, active, strings.TrimSpace(in.Actor), strings.TrimSpace(in.ChangeReason))
	t, err := scanCoATemplate(row)
	if err != nil && strings.Contains(err.Error(), "duplicate key") {
		return CoATemplate{}, fmt.Errorf("%w: that customer (or the house default) already has an active template", ErrConflict)
	}
	return t, err
}

func (s *Store) GetCoATemplate(ctx context.Context, id string) (CoATemplate, error) {
	t, err := scanCoATemplate(s.pool.QueryRow(ctx, `SELECT `+coaTemplateCols+` FROM qc_coa_templates WHERE business_id = $1`, strings.TrimSpace(id)))
	if errors.Is(err, pgx.ErrNoRows) {
		return CoATemplate{}, ErrNotFound
	}
	return t, err
}

func (s *Store) ListCoATemplates(ctx context.Context) ([]CoATemplate, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+coaTemplateCols+` FROM qc_coa_templates ORDER BY is_default DESC, customer_ref, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CoATemplate{}
	for rows.Next() {
		t, err := scanCoATemplate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ResolveCoATemplate picks the layout for a certificate: its own template,
// else its customer's active one, else the house default; ok is false when
// none applies and the legacy layout prints.
func (s *Store) ResolveCoATemplate(ctx context.Context, templateRef, customerRef string) (CoATemplate, bool, error) {
	if ref := strings.TrimSpace(templateRef); ref != "" {
		t, err := s.GetCoATemplate(ctx, ref)
		if err == nil {
			return t, true, nil
		}
		if !errors.Is(err, ErrNotFound) {
			return CoATemplate{}, false, err
		}
	}
	t, err := scanCoATemplate(s.pool.QueryRow(ctx, `SELECT `+coaTemplateCols+` FROM qc_coa_templates
		WHERE active AND ((customer_ref <> '' AND customer_ref = $1) OR is_default)
		ORDER BY (customer_ref = $1) DESC LIMIT 1`, strings.TrimSpace(customerRef)))
	if errors.Is(err, pgx.ErrNoRows) {
		return CoATemplate{}, false, nil
	}
	if err != nil {
		return CoATemplate{}, false, err
	}
	return t, true, nil
}

// BatchResult is a batch's latest reading of one parameter.
type BatchResult struct {
	Parameter string
	Value     float64
	Unit      string
	At        time.Time
	Verdict   string
}

/*
BatchResults gathers the latest value of each parameter recorded for a batch:
measurements (with their verdicts), green-grading metrics, cupping session
scores and the physical moisture test. The newest reading wins.
*/
func (s *Store) BatchResults(ctx context.Context, batch string) (map[string]BatchResult, error) {
	out := map[string]BatchResult{}
	keep := func(r BatchResult) {
		r.Parameter = CanonicalParameter(r.Parameter)
		if r.Parameter == "" {
			return
		}
		if cur, ok := out[r.Parameter]; !ok || r.At.After(cur.At) {
			out[r.Parameter] = r
		}
	}
	rows, err := s.pool.Query(ctx, `
		SELECT parameter, value_num, unit, created_at, verdict FROM qc_lab_measurements
		WHERE batch_business_id = $1 AND value_num IS NOT NULL AND status <> 'void'
		UNION ALL
		SELECT m.key, m.value::float8, '', g.graded_at, g.verdict FROM qc_green_gradings g, jsonb_each_text(g.metrics) m
		WHERE g.batch_business_id = $1 AND g.status <> 'void'
		UNION ALL
		SELECT 'cup_score', total_score, 'pts', created_at, '' FROM qc_cupping_sessions WHERE batch_business_id = $1
		UNION ALL
		SELECT 'moisture', moisture_pct, '%', tested_at, '' FROM qc_physical_tests
		WHERE batch_business_id = $1 AND moisture_pct IS NOT NULL`, strings.TrimSpace(batch))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var r BatchResult
		if err := rows.Scan(&r.Parameter, &r.Value, &r.Unit, &r.At, &r.Verdict); err != nil {
			return nil, err
		}
		keep(r)
	}
	return out, rows.Err()
}
