package store

import (
	"context"
	"strings"
	"time"
)

// InProcessCheck is a quality check taken at a process stage (migration 012).
//
// Its own table rather than an extension of qc_compliance_logs — see the
// migration header for the four reasons, the sharpest of which is that
// calendar.go promotes any compliance log whose log_type matches '%audit%' into
// the calendar.
type InProcessCheck struct {
	BusinessID string         `json:"business_id"`
	CheckDate  *string        `json:"check_date,omitempty"`
	Stage      string         `json:"stage"`
	BatchRef   string         `json:"batch_ref"`
	Parameter  string         `json:"parameter"`
	Target     string         `json:"target"`
	Actual     string         `json:"actual"`
	CheckedBy  string         `json:"checked_by"`
	Result     string         `json:"result"`
	Status     string         `json:"status"`
	Notes      string         `json:"notes"`
	Attrs      map[string]any `json:"attrs"`
	// Migration 016 — see LabMeasurement.
	Verdict        string            `json:"verdict"`
	Evaluation     []EvaluationEntry `json:"evaluation"`
	OverrideReason string            `json:"override_reason"`
	UpdatedBy      string            `json:"updated_by"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
	AutoActions    *AutoActions      `json:"auto_actions,omitempty"`
}

// UpsertInProcessCheckInput is the write shape. Parameter is required — a check
// of nothing is not a check. Optional fields follow optional.go.
type UpsertInProcessCheckInput struct {
	BusinessID string
	Parameter  string
	CheckDate  *string
	Stage      *string
	BatchRef   *string
	Target     *string
	Actual     *string
	CheckedBy  *string
	Result     *string
	Status     *string
	Notes      *string
	Attrs      map[string]any
	// OverrideReason follows optional.go; Actor is who is saving.
	OverrideReason *string
	Actor          string
}

const inProcessCols = `business_id, check_date::text, stage, batch_ref, parameter, target, actual,
	checked_by, result, status, notes, attrs, verdict, evaluation, override_reason, updated_by,
	created_at, updated_at`

func (s *Store) ListInProcessChecks(ctx context.Context, batchRef, status string, limit int) ([]InProcessCheck, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+inProcessCols+`
		FROM qc_in_process_checks
		WHERE ($1 = '' OR batch_ref = $1) AND ($2 = '' OR status = $2)
		ORDER BY check_date DESC NULLS LAST, updated_at DESC
		LIMIT $3`, strings.TrimSpace(batchRef), strings.TrimSpace(status), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []InProcessCheck{}
	for rows.Next() {
		item, err := scanInProcessCheck(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) getInProcessCheck(ctx context.Context, id string) (InProcessCheck, bool, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+inProcessCols+` FROM qc_in_process_checks WHERE business_id = $1`, id)
	if err != nil {
		return InProcessCheck{}, false, err
	}
	defer rows.Close()
	if !rows.Next() {
		return InProcessCheck{}, false, rows.Err()
	}
	item, err := scanInProcessCheck(rows)
	return item, err == nil, err
}

/*
UpsertInProcessCheck creates or updates a check by business_id, preserving any
column the caller did not send.

The actual reading is judged against the 'in_process' specs for the parameter
(016), using the row as it will be after the merge — see
UpsertIncomingInspection. The check's own stage column is a process step
("Roasting"), not a spec stage, so every check resolves as in_process.
*/
func (s *Store) UpsertInProcessCheck(ctx context.Context, in UpsertInProcessCheckInput) (InProcessCheck, error) {
	parameter := strings.TrimSpace(in.Parameter)
	if parameter == "" {
		return InProcessCheck{}, ErrBadInput
	}
	id := strings.TrimSpace(in.BusinessID)
	var prev InProcessCheck
	var err error
	if id == "" {
		if id, err = nextBusinessID(ctx, s.pool, "qc_in_process_checks", "IPC"); err != nil {
			return InProcessCheck{}, err
		}
	} else if prev, _, err = s.getInProcessCheck(ctx, id); err != nil {
		return InProcessCheck{}, err
	}

	actual := prev.Actual
	if in.Actual != nil {
		actual = strings.TrimSpace(*in.Actual)
	}
	grade := attrString(in.Attrs, "grade")
	if grade == "" {
		grade = attrString(prev.Attrs, "grade")
	}
	ev, err := s.Evaluate(ctx, "in_process", grade, []Reading{
		{Parameter: parameter, Raw: actual, Value: parseMeasurementValue(actual)},
	})
	if err != nil {
		return InProcessCheck{}, err
	}
	override := prev.OverrideReason
	if in.OverrideReason != nil {
		override = *in.OverrideReason
	}
	result, err := reconcileResult(trimOptional(in.Result), ev.Verdict, override, func(v string) string {
		if v == "fail" {
			return "out_of_control"
		}
		return "in_control"
	})
	if err != nil {
		return InProcessCheck{}, err
	}

	rows, err := s.pool.Query(ctx, `
		INSERT INTO qc_in_process_checks (
			business_id, check_date, stage, batch_ref, parameter, target, actual,
			checked_by, result, status, notes, attrs,
			verdict, evaluation, override_reason, updated_by, updated_at
		) VALUES (
			$1, COALESCE(NULLIF($2::text, '')::date, CURRENT_DATE), COALESCE($3::text, ''),
			COALESCE($4::text, ''), $5, COALESCE($6::text, ''), COALESCE($7::text, ''),
			COALESCE($8::text, ''), COALESCE($9::text, ''), COALESCE($10::text, 'open'),
			COALESCE($11::text, ''), COALESCE($12::jsonb, '{}'::jsonb),
			$13, $14::jsonb, COALESCE($15::text, ''), $16, NOW()
		)
		ON CONFLICT (business_id) DO UPDATE SET
			check_date = CASE WHEN $2::text IS NULL THEN qc_in_process_checks.check_date
			                  WHEN $2::text = '' THEN NULL
			                  ELSE $2::date END,
			stage = COALESCE($3::text, qc_in_process_checks.stage),
			batch_ref = COALESCE($4::text, qc_in_process_checks.batch_ref),
			parameter = EXCLUDED.parameter,
			target = COALESCE($6::text, qc_in_process_checks.target),
			actual = COALESCE($7::text, qc_in_process_checks.actual),
			checked_by = COALESCE($8::text, qc_in_process_checks.checked_by),
			result = COALESCE($9::text, qc_in_process_checks.result),
			status = COALESCE($10::text, qc_in_process_checks.status),
			notes = COALESCE($11::text, qc_in_process_checks.notes),
			attrs = CASE WHEN $12::jsonb IS NULL THEN qc_in_process_checks.attrs
			             ELSE qc_in_process_checks.attrs || $12::jsonb END,
			verdict = EXCLUDED.verdict,
			evaluation = EXCLUDED.evaluation,
			override_reason = COALESCE($15::text, qc_in_process_checks.override_reason),
			updated_by = EXCLUDED.updated_by,
			updated_at = NOW()
		RETURNING `+inProcessCols,
		id, in.CheckDate, trimOptional(in.Stage), trimOptional(in.BatchRef), parameter,
		in.Target, in.Actual, trimOptional(in.CheckedBy), result,
		blankToNil(in.Status), in.Notes, attrsOptional(in.Attrs),
		ev.Verdict, ev.EntriesJSON(), trimOptional(in.OverrideReason), strings.TrimSpace(in.Actor),
	)
	if err != nil {
		return InProcessCheck{}, err
	}
	item, err := func() (InProcessCheck, error) {
		defer rows.Close()
		if !rows.Next() {
			if err := rows.Err(); err != nil {
				return InProcessCheck{}, err
			}
			return InProcessCheck{}, ErrNotFound
		}
		return scanInProcessCheck(rows)
	}()
	if err != nil {
		return InProcessCheck{}, err
	}
	auto, err := s.raiseAutoActions(ctx, autoActionInput{
		SourceRef: item.BusinessID, SourceKind: "in-process check", HoldRef: item.BatchRef,
		Actor: strings.TrimSpace(in.Actor), Evaluation: ev,
	})
	if err != nil {
		return item, err
	}
	item.AutoActions = auto
	return item, nil
}

func scanInProcessCheck(rows interface{ Scan(...any) error }) (InProcessCheck, error) {
	var item InProcessCheck
	var attrs, evaluation []byte
	if err := rows.Scan(&item.BusinessID, &item.CheckDate, &item.Stage, &item.BatchRef,
		&item.Parameter, &item.Target, &item.Actual, &item.CheckedBy, &item.Result,
		&item.Status, &item.Notes, &attrs, &item.Verdict, &evaluation, &item.OverrideReason,
		&item.UpdatedBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return InProcessCheck{}, err
	}
	item.Attrs = attrsMap(attrs)
	item.Evaluation = scanEvaluation(evaluation)
	return item, nil
}
