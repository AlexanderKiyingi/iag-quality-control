package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// AlertRoute says where one kind of alert goes (migration 026).
type AlertRoute struct {
	Kind         string    `json:"kind"`
	Description  string    `json:"description"`
	Audience     string    `json:"audience"`
	Channel      string    `json:"channel"`
	Recipient    string    `json:"recipient"`
	Enabled      bool      `json:"enabled"`
	UpdatedBy    string    `json:"updated_by"`
	ChangeReason string    `json:"change_reason"`
	UpdatedAt    time.Time `json:"updated_at"`
}

const alertRouteCols = `kind, description, audience, channel, recipient, enabled, updated_by, change_reason, updated_at`

func scanAlertRoute(row interface{ Scan(...any) error }) (AlertRoute, error) {
	var r AlertRoute
	err := row.Scan(&r.Kind, &r.Description, &r.Audience, &r.Channel, &r.Recipient, &r.Enabled, &r.UpdatedBy, &r.ChangeReason, &r.UpdatedAt)
	return r, err
}

func (s *Store) ListAlertRoutes(ctx context.Context) ([]AlertRoute, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+alertRouteCols+` FROM qc_alert_routes ORDER BY kind`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AlertRoute{}
	for rows.Next() {
		r, err := scanAlertRoute(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetAlertRoute returns the route for a kind, or ErrNotFound.
func (s *Store) GetAlertRoute(ctx context.Context, kind string) (AlertRoute, error) {
	r, err := scanAlertRoute(s.pool.QueryRow(ctx, `SELECT `+alertRouteCols+` FROM qc_alert_routes WHERE kind = $1`, strings.TrimSpace(kind)))
	if errors.Is(err, pgx.ErrNoRows) {
		return AlertRoute{}, ErrNotFound
	}
	return r, err
}

// UpdateAlertRoute changes where a kind of alert goes; it says why.
func (s *Store) UpdateAlertRoute(ctx context.Context, in AlertRoute, actor string) (AlertRoute, error) {
	if strings.TrimSpace(in.ChangeReason) == "" {
		return AlertRoute{}, ErrReasonRequired
	}
	if strings.TrimSpace(in.Audience) == "" && strings.TrimSpace(in.Recipient) == "" {
		return AlertRoute{}, ErrBadInput
	}
	r, err := scanAlertRoute(s.pool.QueryRow(ctx, `
		UPDATE qc_alert_routes SET audience = $2, channel = $3, recipient = $4, enabled = $5, updated_by = $6,
			change_reason = $7, updated_at = NOW()
		WHERE kind = $1 RETURNING `+alertRouteCols,
		strings.TrimSpace(in.Kind), strings.TrimSpace(in.Audience), strings.TrimSpace(in.Channel), strings.TrimSpace(in.Recipient),
		in.Enabled, strings.TrimSpace(actor), strings.TrimSpace(in.ChangeReason)))
	if errors.Is(err, pgx.ErrNoRows) {
		return AlertRoute{}, ErrNotFound
	}
	return r, err
}
