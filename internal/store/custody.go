package store

import (
	"context"
	"strings"
	"time"
)

type CustodyLog struct {
	ID               int64     `json:"id"`
	SampleBusinessID string    `json:"sample_business_id"`
	Action           string    `json:"action"`
	Actor            string    `json:"actor"`
	Location         string    `json:"location"`
	Notes            string    `json:"notes"`
	LoggedAt         time.Time `json:"logged_at"`
}

type CreateCustodyLogInput struct {
	SampleBusinessID string
	Action           string
	Actor            string
	Location         string
	Notes            string
}

func (s *Store) CreateCustodyLog(ctx context.Context, in CreateCustodyLogInput) (CustodyLog, error) {
	action := strings.TrimSpace(in.Action)
	if action == "" || strings.TrimSpace(in.SampleBusinessID) == "" {
		return CustodyLog{}, ErrBadInput
	}
	if _, err := s.GetSample(ctx, in.SampleBusinessID); err != nil {
		return CustodyLog{}, err
	}
	var out CustodyLog
	err := s.pool.QueryRow(ctx, `
		INSERT INTO qc_custody_logs (sample_business_id, action, actor, location, notes)
		VALUES ($1,$2,$3,$4,$5)
		RETURNING id, sample_business_id, action, actor, location, notes, logged_at`,
		in.SampleBusinessID, action, strings.TrimSpace(in.Actor),
		strings.TrimSpace(in.Location), strings.TrimSpace(in.Notes),
	).Scan(&out.ID, &out.SampleBusinessID, &out.Action, &out.Actor, &out.Location, &out.Notes, &out.LoggedAt)
	return out, err
}

func (s *Store) ListCustodyLogs(ctx context.Context, sampleID string, limit int) ([]CustodyLog, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, sample_business_id, action, actor, location, notes, logged_at
		FROM qc_custody_logs
		WHERE sample_business_id = $1
		ORDER BY logged_at DESC LIMIT $2`, sampleID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CustodyLog
	for rows.Next() {
		var item CustodyLog
		if err := rows.Scan(&item.ID, &item.SampleBusinessID, &item.Action, &item.Actor, &item.Location, &item.Notes, &item.LoggedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
