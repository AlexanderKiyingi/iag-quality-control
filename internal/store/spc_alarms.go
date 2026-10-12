package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"iag-quality-control/backend/internal/domain"
)

// SPCSettings is how one parameter is watched (migration 022).
type SPCSettings struct {
	Parameter    string    `json:"parameter"`
	Rules        []int     `json:"rules"`
	Alarms       bool      `json:"alarms"`
	RaiseNC      bool      `json:"raise_nc"`
	UpdatedBy    string    `json:"updated_by"`
	ChangeReason string    `json:"change_reason"`
	UpdatedAt    time.Time `json:"updated_at"`
	// Default is true when no row exists and these are the defaults.
	Default bool `json:"default"`
}

// SPCAlert is one rule broken by one reading.
type SPCAlert struct {
	ID              int64      `json:"id"`
	Parameter       string     `json:"parameter"`
	Rule            int        `json:"rule"`
	RuleName        string     `json:"rule_name"`
	Ref             string     `json:"ref"`
	BatchBusinessID string     `json:"batch_business_id"`
	Value           float64    `json:"value"`
	Center          float64    `json:"center"`
	UCL             float64    `json:"ucl"`
	LCL             float64    `json:"lcl"`
	ReadingAt       string     `json:"reading_at"`
	NonConformance  string     `json:"non_conformance,omitempty"`
	Status          string     `json:"status"`
	AcknowledgedBy  string     `json:"acknowledged_by,omitempty"`
	AckNote         string     `json:"ack_note,omitempty"`
	AcknowledgedAt  *time.Time `json:"acknowledged_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}

func (s *Store) GetSPCSettings(ctx context.Context, parameter string) (SPCSettings, error) {
	parameter = CanonicalParameter(parameter)
	if parameter == "" {
		return SPCSettings{}, ErrBadInput
	}
	var out SPCSettings
	var rules []int32
	err := s.pool.QueryRow(ctx, `
		SELECT parameter, rules, alarms, raise_nc, updated_by, change_reason, updated_at
		FROM qc_spc_settings WHERE parameter = $1`, parameter).
		Scan(&out.Parameter, &rules, &out.Alarms, &out.RaiseNC, &out.UpdatedBy, &out.ChangeReason, &out.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return SPCSettings{Parameter: parameter, Rules: append([]int(nil), domain.DefaultRules...), Alarms: true, Default: true}, nil
	}
	if err != nil {
		return SPCSettings{}, err
	}
	out.Rules = make([]int, len(rules))
	for i, r := range rules {
		out.Rules[i] = int(r)
	}
	return out, nil
}

// PutSPCSettings replaces how a parameter is watched; a change needs a reason.
func (s *Store) PutSPCSettings(ctx context.Context, in SPCSettings, actor string) (SPCSettings, error) {
	parameter := CanonicalParameter(in.Parameter)
	if parameter == "" {
		return SPCSettings{}, ErrBadInput
	}
	rules := []int32{}
	for _, r := range in.Rules {
		if _, ok := domain.Rules[r]; !ok {
			return SPCSettings{}, fmt.Errorf("%w: rule %d is not one of 1, 2, 3, 5, 6", ErrBadInput, r)
		}
		rules = append(rules, int32(r))
	}
	current, err := s.GetSPCSettings(ctx, parameter)
	if err != nil {
		return SPCSettings{}, err
	}
	if !current.Default && strings.TrimSpace(in.ChangeReason) == "" {
		return SPCSettings{}, ErrReasonRequired
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO qc_spc_settings (parameter, rules, alarms, raise_nc, updated_by, change_reason, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW())
		ON CONFLICT (parameter) DO UPDATE SET rules = EXCLUDED.rules, alarms = EXCLUDED.alarms,
			raise_nc = EXCLUDED.raise_nc, updated_by = EXCLUDED.updated_by,
			change_reason = EXCLUDED.change_reason, updated_at = NOW()`,
		parameter, rules, in.Alarms, in.RaiseNC, strings.TrimSpace(actor), strings.TrimSpace(in.ChangeReason)); err != nil {
		return SPCSettings{}, err
	}
	return s.GetSPCSettings(ctx, parameter)
}

/*
CheckSPCAlarms tests the newest reading of each parameter that ref recorded
against its I-MR chart (30 days, the parameter's rules) and files an alarm for
every rule that reading completes. Only the reading just recorded is judged:
an older break is history, and was alarmed when it happened.

A break on a parameter set to raise_nc also raises a flag-level
non-conformance through the same path a failing spec uses, keyed on the alarm
so it is raised once.
*/
func (s *Store) CheckSPCAlarms(ctx context.Context, ref string, parameters []string, actor string) ([]SPCAlert, error) {
	var out []SPCAlert
	seen := map[string]bool{}
	for _, raw := range parameters {
		parameter := CanonicalParameter(raw)
		if parameter == "" || seen[parameter] {
			continue
		}
		seen[parameter] = true
		settings, err := s.GetSPCSettings(ctx, parameter)
		if err != nil {
			return out, err
		}
		if !settings.Alarms || len(settings.Rules) == 0 {
			continue
		}
		series, err := s.SPC(ctx, SPCOptions{Metric: parameter, Days: 30, Rules: settings.Rules})
		if err != nil {
			return out, err
		}
		idx := -1
		for i, p := range series.Points {
			if p.Ref == ref {
				idx = i
			}
		}
		if idx < 0 || len(series.Points[idx].Rules) == 0 {
			continue
		}
		p := series.Points[idx]
		for _, rule := range p.Rules {
			var a SPCAlert
			err := s.pool.QueryRow(ctx, `
				INSERT INTO qc_spc_alerts (parameter, rule, ref, batch_business_id, value, center, ucl, lcl, reading_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
				ON CONFLICT (parameter, ref, rule) DO NOTHING
				RETURNING id, created_at`,
				parameter, rule, p.Ref, p.Batch, p.Value, series.Mean, series.UCL, series.LCL, p.Timestamp).
				Scan(&a.ID, &a.CreatedAt)
			if errors.Is(err, pgx.ErrNoRows) {
				continue // alarmed before
			}
			if err != nil {
				return out, err
			}
			a.Parameter, a.Rule, a.RuleName, a.Ref, a.BatchBusinessID = parameter, rule, domain.Rules[rule], p.Ref, p.Batch
			a.Value, a.Center, a.UCL, a.LCL, a.ReadingAt, a.Status = p.Value, series.Mean, series.UCL, series.LCL, p.Timestamp, "open"
			if settings.RaiseNC {
				v := p.Value
				auto, err := s.raiseAutoActions(ctx, autoActionInput{
					SourceRef:  fmt.Sprintf("SPC-%d", a.ID),
					SourceKind: fmt.Sprintf("SPC rule %d (%s) on %s", rule, domain.Rules[rule], p.Ref),
					HoldRef:    p.Batch,
					Actor:      actor,
					Evaluation: Evaluation{Verdict: domain.VerdictFail, Action: domain.ActionFlag,
						Entries: []EvaluationEntry{{Parameter: parameter, Value: &v, Verdict: domain.VerdictFail, Action: domain.ActionFlag,
							Raw: fmt.Sprintf("%g, control limits %g–%g", v, series.LCL, series.UCL)}}},
				})
				if err != nil {
					return out, err
				}
				if auto != nil && auto.NonConformanceID != "" {
					a.NonConformance = auto.NonConformanceID
					if _, err := s.pool.Exec(ctx, `UPDATE qc_spc_alerts SET non_conformance = $2 WHERE id = $1`, a.ID, a.NonConformance); err != nil {
						return out, err
					}
				}
			}
			out = append(out, a)
		}
	}
	return out, nil
}

func (s *Store) ListSPCAlerts(ctx context.Context, status, parameter string, limit int) ([]SPCAlert, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, parameter, rule, ref, batch_business_id, value, center, ucl, lcl, reading_at, non_conformance,
		       status, acknowledged_by, ack_note, acknowledged_at, created_at
		FROM qc_spc_alerts
		WHERE ($1 = '' OR status = $1) AND ($2 = '' OR parameter = $2)
		ORDER BY created_at DESC LIMIT $3`, strings.TrimSpace(status), CanonicalParameter(parameter), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SPCAlert{}
	for rows.Next() {
		var a SPCAlert
		if err := rows.Scan(&a.ID, &a.Parameter, &a.Rule, &a.Ref, &a.BatchBusinessID, &a.Value, &a.Center, &a.UCL,
			&a.LCL, &a.ReadingAt, &a.NonConformance, &a.Status, &a.AcknowledgedBy, &a.AckNote, &a.AcknowledgedAt,
			&a.CreatedAt); err != nil {
			return nil, err
		}
		a.RuleName = domain.Rules[a.Rule]
		out = append(out, a)
	}
	return out, rows.Err()
}

// AcknowledgeSPCAlert records that someone looked, and what they concluded.
func (s *Store) AcknowledgeSPCAlert(ctx context.Context, id int64, actor, note string) error {
	if strings.TrimSpace(note) == "" {
		return ErrReasonRequired
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE qc_spc_alerts SET status = 'acknowledged', acknowledged_by = $2, ack_note = $3, acknowledged_at = NOW()
		WHERE id = $1 AND status = 'open'`, id, strings.TrimSpace(actor), strings.TrimSpace(note))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		var exists bool
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM qc_spc_alerts WHERE id = $1)`, id).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
		return fmt.Errorf("%w: already acknowledged", ErrConflict)
	}
	return nil
}
