package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
)

type Technician struct {
	BusinessID     string   `json:"business_id"`
	Name           string   `json:"name"`
	Role           string   `json:"role"`
	Level          string   `json:"level"`
	Color          string   `json:"color"`
	Certifications []string `json:"certifications"`
	Active         bool     `json:"active"`
}

type UpsertTechnicianInput struct {
	BusinessID     string
	Name           string
	Role           string
	Level          string
	Color          string
	Certifications []string
	Active         *bool
}

func (s *Store) ListTechnicians(ctx context.Context, activeOnly bool) ([]Technician, error) {
	q := `
		SELECT business_id, name, role, level, color, certifications, active
		FROM qc_technicians`
	if activeOnly {
		q += ` WHERE active = true`
	}
	q += ` ORDER BY name ASC`

	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTechnicians(rows)
}

func (s *Store) GetTechnician(ctx context.Context, businessID string) (Technician, error) {
	var out Technician
	var certsJSON []byte
	err := s.pool.QueryRow(ctx, `
		SELECT business_id, name, role, level, color, certifications, active
		FROM qc_technicians WHERE business_id = $1`, businessID,
	).Scan(&out.BusinessID, &out.Name, &out.Role, &out.Level, &out.Color, &certsJSON, &out.Active)
	if errors.Is(err, pgx.ErrNoRows) {
		return Technician{}, ErrNotFound
	}
	if err != nil {
		return Technician{}, err
	}
	_ = json.Unmarshal(certsJSON, &out.Certifications)
	return out, nil
}

func (s *Store) UpsertTechnician(ctx context.Context, in UpsertTechnicianInput) (Technician, error) {
	id := strings.TrimSpace(in.BusinessID)
	name := strings.TrimSpace(in.Name)
	if id == "" || name == "" {
		return Technician{}, ErrBadInput
	}
	certs := in.Certifications
	if certs == nil {
		certs = []string{}
	}
	certsJSON, err := json.Marshal(certs)
	if err != nil {
		return Technician{}, err
	}
	active := true
	if in.Active != nil {
		active = *in.Active
	}
	color := strings.TrimSpace(in.Color)
	if color == "" {
		color = "#0e6b5f"
	}
	var out Technician
	err = s.pool.QueryRow(ctx, `
		INSERT INTO qc_technicians (business_id, name, role, level, color, certifications, active)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (business_id) DO UPDATE SET
			name = EXCLUDED.name,
			role = EXCLUDED.role,
			level = EXCLUDED.level,
			color = EXCLUDED.color,
			certifications = EXCLUDED.certifications,
			active = EXCLUDED.active
		RETURNING business_id, name, role, level, color, certifications, active`,
		id, name, strings.TrimSpace(in.Role), strings.TrimSpace(in.Level), color, certsJSON, active,
	).Scan(&out.BusinessID, &out.Name, &out.Role, &out.Level, &out.Color, &certsJSON, &out.Active)
	if err != nil {
		return Technician{}, err
	}
	_ = json.Unmarshal(certsJSON, &out.Certifications)
	return out, nil
}

func scanTechnicians(rows pgx.Rows) ([]Technician, error) {
	defer rows.Close()
	var out []Technician
	for rows.Next() {
		var item Technician
		var certsJSON []byte
		if err := rows.Scan(&item.BusinessID, &item.Name, &item.Role, &item.Level, &item.Color, &certsJSON, &item.Active); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(certsJSON, &item.Certifications)
		out = append(out, item)
	}
	return out, rows.Err()
}
