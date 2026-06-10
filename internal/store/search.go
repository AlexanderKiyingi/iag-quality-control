package store

import (
	"context"
	"fmt"
	"strings"
)

type SearchHit struct {
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Label   string `json:"label"`
	Detail  string `json:"detail,omitempty"`
}

func (s *Store) Search(ctx context.Context, query string, limit int) ([]SearchHit, error) {
	q := strings.TrimSpace(query)
	if len(q) < 2 {
		return nil, ErrBadInput
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	pattern := "%" + q + "%"

	var out []SearchHit

	rows, err := s.pool.Query(ctx, `
		SELECT 'sample', business_id, batch_business_id, COALESCE(sample_type, '')
		FROM qc_samples
		WHERE business_id ILIKE $1 OR batch_business_id ILIKE $1 OR notes ILIKE $1
		ORDER BY received_at DESC LIMIT $2`, pattern, limit)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var kind, id, label, detail string
		if err := rows.Scan(&kind, &id, &label, &detail); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, SearchHit{Kind: kind, ID: id, Label: label, Detail: detail})
	}
	rows.Close()

	rows, err = s.pool.Query(ctx, `
		SELECT 'batch_lab', batch_business_id, COALESCE(grade, ''), COALESCE(tester, '')
		FROM qc_batch_lab_summary
		WHERE batch_business_id ILIKE $1 OR grade ILIKE $1 OR tester ILIKE $1
		LIMIT $2`, pattern, limit)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var kind, id, label, detail string
		if err := rows.Scan(&kind, &id, &label, &detail); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, SearchHit{Kind: kind, ID: id, Label: id, Detail: fmt.Sprintf("%s · %s", label, detail)})
	}
	rows.Close()

	rows, err = s.pool.Query(ctx, `
		SELECT 'coa', coa_number, lot_business_id, COALESCE(batch_business_id, '')
		FROM qc_coa
		WHERE coa_number ILIKE $1 OR lot_business_id ILIKE $1 OR batch_business_id ILIKE $1
		ORDER BY issued_at DESC LIMIT $2`, pattern, limit)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var kind, id, label, detail string
		if err := rows.Scan(&kind, &id, &label, &detail); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, SearchHit{Kind: kind, ID: id, Label: label, Detail: detail})
	}
	rows.Close()

	rows, err = s.pool.Query(ctx, `
		SELECT 'instrument', business_id, name, COALESCE(location, '')
		FROM qc_instruments
		WHERE business_id ILIKE $1 OR name ILIKE $1 OR instrument_type ILIKE $1 OR location ILIKE $1
		LIMIT $2`, pattern, limit)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var kind, id, label, detail string
		if err := rows.Scan(&kind, &id, &label, &detail); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, SearchHit{Kind: kind, ID: id, Label: label, Detail: detail})
	}
	rows.Close()

	rows, err = s.pool.Query(ctx, `
		SELECT 'certification', business_id, batch_business_id, COALESCE(stage, '')
		FROM qc_certification_requests
		WHERE business_id ILIKE $1 OR batch_business_id ILIKE $1 OR lot_business_id ILIKE $1
		ORDER BY requested_at DESC LIMIT $2`, pattern, limit)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var kind, id, label, detail string
		if err := rows.Scan(&kind, &id, &label, &detail); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, SearchHit{Kind: kind, ID: id, Label: label, Detail: detail})
	}
	rows.Close()

	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
