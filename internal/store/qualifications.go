package store

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
)

// MethodSOP is a lab method's controlled SOP and the version in force.
type MethodSOP struct {
	MethodID   string `json:"method_id"`
	MethodName string `json:"method_name"`
	SopRef     string `json:"sop_ref"`
	// EffectiveVersion is 0 when the SOP has no version in force.
	EffectiveVersion int `json:"effective_version"`
}

// LabMethodSOPs lists registered lab methods with their SOP and its version
// in force — the columns of the qualification matrix.
func (s *Store) LabMethodSOPs(ctx context.Context, methodID string) ([]MethodSOP, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT m.business_id, m.name, m.sop_ref,
		       COALESCE((SELECT v.version FROM qc_document_versions v
		                 WHERE v.doc_business_id = upper(m.sop_ref) AND v.status = 'effective'), 0)
		FROM qc_lab_methods m
		WHERE ($1 = '' OR m.business_id = $1) AND m.status <> 'superseded'
		ORDER BY m.business_id`, strings.TrimSpace(methodID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MethodSOP{}
	for rows.Next() {
		var m MethodSOP
		if err := rows.Scan(&m.MethodID, &m.MethodName, &m.SopRef, &m.EffectiveVersion); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// HasReadSOP reports whether email acknowledged the given version of a
// document (025).
func (s *Store) HasReadSOP(ctx context.Context, sopRef string, version int, email string) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM qc_document_acknowledgements
		WHERE doc_business_id = upper($1) AND version = $2 AND user_email = lower($3))`,
		strings.TrimSpace(sopRef), version, strings.TrimSpace(email)).Scan(&ok)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return ok, err
}

// SOPReaders maps document → version → readers' emails, for the matrix.
func (s *Store) SOPReaders(ctx context.Context) (map[string]map[int]map[string]bool, error) {
	rows, err := s.pool.Query(ctx, `SELECT doc_business_id, version, user_email FROM qc_document_acknowledgements`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]map[int]map[string]bool{}
	for rows.Next() {
		var doc, email string
		var v int
		if err := rows.Scan(&doc, &v, &email); err != nil {
			return nil, err
		}
		if out[doc] == nil {
			out[doc] = map[int]map[string]bool{}
		}
		if out[doc][v] == nil {
			out[doc][v] = map[string]bool{}
		}
		out[doc][v][strings.ToLower(email)] = true
	}
	return out, rows.Err()
}
