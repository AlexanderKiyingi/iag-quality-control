package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

func (s *Store) CreateCoA(ctx context.Context, in CreateCoAInput) (CoA, error) {
	lotID := strings.TrimSpace(in.LotBusinessID)
	coaNo := strings.TrimSpace(in.CoaNumber)
	if lotID == "" || coaNo == "" {
		return CoA{}, ErrBadInput
	}
	var out CoA
	err := s.pool.QueryRow(ctx, `
		INSERT INTO qc_coa (coa_number, lot_business_id, batch_business_id, document_ref, issued_by)
		VALUES ($1,$2,$3,$4,$5)
		RETURNING coa_number, lot_business_id, batch_business_id, document_ref, issued_by, issued_at, status`,
		coaNo, lotID, strings.TrimSpace(in.BatchBusinessID), strings.TrimSpace(in.DocumentRef), strings.TrimSpace(in.IssuedBy),
	).Scan(
		&out.CoaNumber, &out.LotBusinessID, &out.BatchBusinessID, &out.DocumentRef, &out.IssuedBy, &out.IssuedAt, &out.Status,
	)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique") {
			return CoA{}, ErrConflict
		}
		return CoA{}, fmt.Errorf("create coa: %w", err)
	}
	return out, nil
}

func (s *Store) ListCoA(ctx context.Context, lotID string, limit int) ([]CoA, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := `
		SELECT coa_number, lot_business_id, batch_business_id, document_ref, issued_by, issued_at, status
		FROM qc_coa`
	args := []any{}
	if lotID != "" {
		q += " WHERE lot_business_id = $1"
		args = append(args, lotID)
		q += " ORDER BY issued_at DESC LIMIT $2"
		args = append(args, limit)
	} else {
		q += " ORDER BY issued_at DESC LIMIT $1"
		args = append(args, limit)
	}
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []CoA
	for rows.Next() {
		var item CoA
		if err := rows.Scan(
			&item.CoaNumber, &item.LotBusinessID, &item.BatchBusinessID, &item.DocumentRef,
			&item.IssuedBy, &item.IssuedAt, &item.Status,
		); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) GetCoAByNumber(ctx context.Context, coaNumber string) (CoA, error) {
	var out CoA
	err := s.pool.QueryRow(ctx, `
		SELECT coa_number, lot_business_id, batch_business_id, document_ref, issued_by, issued_at, status
		FROM qc_coa WHERE coa_number = $1`, coaNumber,
	).Scan(
		&out.CoaNumber, &out.LotBusinessID, &out.BatchBusinessID, &out.DocumentRef,
		&out.IssuedBy, &out.IssuedAt, &out.Status,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return CoA{}, ErrNotFound
	}
	return out, err
}

func (s *Store) GetCoAByLot(ctx context.Context, lotID string) (CoA, error) {
	var out CoA
	err := s.pool.QueryRow(ctx, `
		SELECT coa_number, lot_business_id, batch_business_id, document_ref, issued_by, issued_at, status
		FROM qc_coa WHERE lot_business_id = $1 AND status = 'active'
		ORDER BY issued_at DESC LIMIT 1`, lotID,
	).Scan(
		&out.CoaNumber, &out.LotBusinessID, &out.BatchBusinessID, &out.DocumentRef,
		&out.IssuedBy, &out.IssuedAt, &out.Status,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return CoA{}, ErrNotFound
	}
	return out, err
}
