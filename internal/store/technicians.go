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

// UpsertTechnicianInput is the write shape for a technician. BusinessID and
// Name are required; the rest follow the nil/""/value contract in optional.go,
// and a nil Certifications means "not sent" rather than "none".
type UpsertTechnicianInput struct {
	BusinessID     string
	Name           string
	Role           *string
	Level          *string
	Color          *string
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

// UpsertTechnician creates or updates a technician, preserving any column the
// caller did not send.
//
// It used to replace every column from EXCLUDED with `active` collapsing to
// true whenever it was absent, so any partial save — correcting a name, adding
// a certification — silently reactivated a technician who had been stood down.
func (s *Store) UpsertTechnician(ctx context.Context, in UpsertTechnicianInput) (Technician, error) {
	id := strings.TrimSpace(in.BusinessID)
	name := strings.TrimSpace(in.Name)
	if id == "" || name == "" {
		return Technician{}, ErrBadInput
	}
	// nil means the caller did not send the list at all; an explicitly empty
	// list still clears the certifications.
	var certsJSON []byte
	if in.Certifications != nil {
		var err error
		if certsJSON, err = json.Marshal(in.Certifications); err != nil {
			return Technician{}, err
		}
	}
	var out Technician
	var scanned []byte
	err := s.pool.QueryRow(ctx, `
		INSERT INTO qc_technicians (business_id, name, role, level, color, certifications, active)
		VALUES (
			$1, $2, COALESCE($3::text, ''), COALESCE($4::text, ''),
			COALESCE($5::text, '#0e6b5f'), COALESCE($6::jsonb, '[]'::jsonb),
			COALESCE($7::boolean, true)
		)
		ON CONFLICT (business_id) DO UPDATE SET
			name = EXCLUDED.name,
			role = COALESCE($3::text, qc_technicians.role),
			level = COALESCE($4::text, qc_technicians.level),
			color = COALESCE($5::text, qc_technicians.color),
			certifications = COALESCE($6::jsonb, qc_technicians.certifications),
			active = COALESCE($7::boolean, qc_technicians.active)
		RETURNING business_id, name, role, level, color, certifications, active`,
		id, name, trimOptional(in.Role), trimOptional(in.Level), blankToNil(in.Color),
		certsJSON, in.Active,
	).Scan(&out.BusinessID, &out.Name, &out.Role, &out.Level, &out.Color, &scanned, &out.Active)
	if err != nil {
		return Technician{}, err
	}
	_ = json.Unmarshal(scanned, &out.Certifications)
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
