package store

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// LabMeasurement is one parameterised result against a sample (migration 013).
type LabMeasurement struct {
	BusinessID       string         `json:"business_id"`
	SampleBusinessID string         `json:"sample_business_id"`
	BatchBusinessID  string         `json:"batch_business_id"`
	Parameter        string         `json:"parameter"`
	ValueText        string         `json:"value_text"`
	ValueNum         *float64       `json:"value_num,omitempty"`
	Unit             string         `json:"unit"`
	SpecLimit        string         `json:"spec_limit"`
	MethodID         string         `json:"method_id"`
	Analyst          string         `json:"analyst"`
	Result           string         `json:"result"`
	Status           string         `json:"status"`
	Notes            string         `json:"notes"`
	Attachments      string         `json:"attachments"`
	ReportedAt       *string        `json:"reported_at,omitempty"`
	Attrs            map[string]any `json:"attrs"`
	// Migration 016: the stage the reading was judged at, the verdict the
	// service computed, the band it was judged against, and why the analyst's
	// result disagrees with it when it does.
	Stage          string            `json:"stage"`
	Verdict        string            `json:"verdict"`
	Evaluation     []EvaluationEntry `json:"evaluation"`
	OverrideReason string            `json:"override_reason"`
	UpdatedBy      string            `json:"updated_by"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
	// What a failing reading set in motion; only on the write response.
	AutoActions *AutoActions `json:"auto_actions,omitempty"`
}

// CreateLabMeasurementInput is the write shape. A measurement is an event, so
// there is nothing to preserve and no upsert.
type CreateLabMeasurementInput struct {
	BusinessID       string
	SampleBusinessID string
	Parameter        string
	Value            string
	Unit             string
	SpecLimit        string
	MethodID         string
	Analyst          string
	Result           string
	Status           string
	Notes            string
	Attachments      string
	ReportedAt       string
	Attrs            map[string]any
	// Stage and Grade pick the spec; both fall back to the sample's attrs.
	Stage          string
	Grade          string
	OverrideReason string
	Actor          string
}

/*
MeasurementRollupField maps a parameter name onto the qc_batch_lab_summary
column it feeds, or "" when it feeds none.

The rollup is read by the dashboard, SPC analytics, the CoA PDF and the
qc.lab.result_recorded payload. Recording results through the measurements table
without mirroring the known metrics into it would quietly stop feeding all four —
a regression against what the fixed-column tests already do. Anything outside
these six is stored as a measurement and nothing more, which is the whole point
of the table.
*/
func MeasurementRollupField(parameter string) string {
	switch strings.ToLower(strings.TrimSpace(parameter)) {
	case "moisture", "moisture %", "moisture_pct", "moisture percent":
		return "moisture"
	case "water activity", "water_activity", "aw", "a_w":
		return "water_activity"
	case "cup score", "cup_score", "cupping score", "sca score", "total score":
		return "cup_score"
	case "defects", "defect count", "defect_count":
		return "defects"
	default:
		return ""
	}
}

// parseMeasurementValue returns the numeric reading when the text is one.
//
// Real results are often "<0.1", "trace" or "pass", and refusing those would
// push users back into the notes field, so value_text always keeps what was
// typed and value_num is filled only when it parses.
func parseMeasurementValue(v string) *float64 {
	trimmed := strings.TrimSpace(v)
	if trimmed == "" {
		return nil
	}
	n, err := strconv.ParseFloat(strings.TrimSuffix(trimmed, "%"), 64)
	if err != nil {
		return nil
	}
	return &n
}

const measurementCols = `business_id, sample_business_id, batch_business_id, parameter, value_text,
	value_num, unit, spec_limit, method_id, analyst, result, status, notes, attachments,
	reported_at::text, attrs, stage, verdict, evaluation, override_reason, updated_by,
	created_at, updated_at`

func (s *Store) ListLabMeasurements(ctx context.Context, sampleID, parameter string, limit int) ([]LabMeasurement, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+measurementCols+`
		FROM qc_lab_measurements
		WHERE ($1 = '' OR sample_business_id = $1)
		  AND ($2 = '' OR parameter ILIKE $2)
		ORDER BY reported_at DESC NULLS LAST, created_at DESC
		LIMIT $3`, strings.TrimSpace(sampleID), strings.TrimSpace(parameter), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LabMeasurement{}
	for rows.Next() {
		item, err := scanMeasurement(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// CreateLabMeasurement records one reading against a sample, and mirrors it into
// the batch rollup when the parameter is one the rollup knows.
//
// The sample must exist: a measurement with no sample cannot be traced to a
// batch, and an untraceable result is not a result.
func (s *Store) CreateLabMeasurement(ctx context.Context, in CreateLabMeasurementInput) (LabMeasurement, error) {
	sampleID := strings.TrimSpace(in.SampleBusinessID)
	parameter := strings.TrimSpace(in.Parameter)
	if sampleID == "" || parameter == "" {
		return LabMeasurement{}, ErrBadInput
	}
	sample, err := s.GetSample(ctx, sampleID)
	if err != nil {
		return LabMeasurement{}, err
	}
	id := strings.TrimSpace(in.BusinessID)
	if id == "" {
		if id, err = nextBusinessID(ctx, s.pool, "qc_lab_measurements", "MSR"); err != nil {
			return LabMeasurement{}, err
		}
	}
	status := strings.TrimSpace(in.Status)
	if status == "" {
		status = "reported"
	}

	// Judge the reading. The analyst's result is kept when it agrees or takes
	// no position; contradicting the limits needs a reason (reconcileResult).
	stage := strings.TrimSpace(in.Stage)
	if stage == "" {
		stage = attrString(sample.Attrs, "stage")
	}
	if stage != "" {
		if stage = NormalizeSpecStage(stage); stage == "" {
			return LabMeasurement{}, fmt.Errorf("%w: stage must be incoming, in_process, finished or any", ErrBadInput)
		}
	}
	grade := strings.TrimSpace(in.Grade)
	if grade == "" {
		grade = attrString(sample.Attrs, "grade")
	}
	value := parseMeasurementValue(in.Value)
	ev, err := s.Evaluate(ctx, stage, grade, []Reading{{Parameter: parameter, Raw: in.Value, Value: value}})
	if err != nil {
		return LabMeasurement{}, err
	}
	var sentResult *string
	if r := strings.TrimSpace(in.Result); r != "" {
		sentResult = &r
	}
	result, err := reconcileResult(sentResult, ev.Verdict, in.OverrideReason, func(v string) string { return v })
	if err != nil {
		return LabMeasurement{}, err
	}
	resultText := ""
	if result != nil {
		resultText = *result
	}

	rows, err := s.pool.Query(ctx, `
		INSERT INTO qc_lab_measurements (
			business_id, sample_business_id, batch_business_id, parameter, value_text, value_num,
			unit, spec_limit, method_id, analyst, result, status, notes, attachments,
			reported_at, attrs, stage, verdict, evaluation, override_reason, updated_by, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14,
			COALESCE(NULLIF($15::text, '')::date, CURRENT_DATE),
			COALESCE($16::jsonb, '{}'::jsonb), $17, $18, $19::jsonb, $20, $21, NOW()
		)
		RETURNING `+measurementCols,
		id, sampleID, sample.BatchBusinessID, parameter, strings.TrimSpace(in.Value),
		value, strings.TrimSpace(in.Unit), strings.TrimSpace(in.SpecLimit),
		strings.TrimSpace(in.MethodID), strings.TrimSpace(in.Analyst), resultText,
		status, in.Notes, in.Attachments, strings.TrimSpace(in.ReportedAt), attrsOptional(in.Attrs),
		stage, ev.Verdict, ev.EntriesJSON(), strings.TrimSpace(in.OverrideReason),
		strings.TrimSpace(in.Actor),
	)
	if err != nil {
		return LabMeasurement{}, err
	}
	item, err := func() (LabMeasurement, error) {
		defer rows.Close()
		if !rows.Next() {
			if err := rows.Err(); err != nil {
				return LabMeasurement{}, err
			}
			return LabMeasurement{}, ErrNotFound
		}
		return scanMeasurement(rows)
	}()
	if err != nil {
		return LabMeasurement{}, err
	}

	// A failing reading on a flag/hold spec raises an NC (and a hold). An
	// override does not suppress it: the analyst may be right that the batch
	// is fine, and the NC is where that judgement is recorded and closed.
	auto, err := s.raiseAutoActions(ctx, autoActionInput{
		SourceRef: item.BusinessID, SourceKind: "measurement", HoldRef: item.BatchBusinessID,
		Actor: strings.TrimSpace(in.Actor), Evaluation: ev,
	})
	if err != nil {
		return item, err
	}
	item.AutoActions = auto

	// Mirror the six metrics the rollup knows, so SPC, the dashboard, the CoA
	// PDF and the lab-result event keep being fed. A failure here would leave a
	// correctly recorded measurement looking like a failed write, so it is
	// surfaced rather than swallowed — the caller decides.
	if field := MeasurementRollupField(item.Parameter); field != "" && item.ValueNum != nil && item.BatchBusinessID != "" {
		roll := UpsertLabSummaryInput{
			BatchBusinessID: item.BatchBusinessID,
			Tester:          item.Analyst,
			LatestSampleID:  item.SampleBusinessID,
		}
		switch field {
		case "moisture":
			roll.Moisture = item.ValueNum
		case "water_activity":
			roll.WaterActivity = item.ValueNum
		case "cup_score":
			roll.CupScore = item.ValueNum
		case "defects":
			defects := int(*item.ValueNum)
			roll.Defects = &defects
		}
		if _, err := s.UpsertBatchLabSummary(ctx, roll); err != nil {
			return item, err
		}
	}
	return item, nil
}

func scanMeasurement(rows interface{ Scan(...any) error }) (LabMeasurement, error) {
	var item LabMeasurement
	var attrs, evaluation []byte
	if err := rows.Scan(&item.BusinessID, &item.SampleBusinessID, &item.BatchBusinessID,
		&item.Parameter, &item.ValueText, &item.ValueNum, &item.Unit, &item.SpecLimit,
		&item.MethodID, &item.Analyst, &item.Result, &item.Status, &item.Notes,
		&item.Attachments, &item.ReportedAt, &attrs, &item.Stage, &item.Verdict, &evaluation,
		&item.OverrideReason, &item.UpdatedBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return LabMeasurement{}, err
	}
	item.Attrs = attrsMap(attrs)
	item.Evaluation = scanEvaluation(evaluation)
	return item, nil
}
