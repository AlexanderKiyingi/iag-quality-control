package store

import (
	"context"
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
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
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
	reported_at::text, attrs, created_at, updated_at`

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
	rows, err := s.pool.Query(ctx, `
		INSERT INTO qc_lab_measurements (
			business_id, sample_business_id, batch_business_id, parameter, value_text, value_num,
			unit, spec_limit, method_id, analyst, result, status, notes, attachments,
			reported_at, attrs, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14,
			COALESCE(NULLIF($15::text, '')::date, CURRENT_DATE),
			COALESCE($16::jsonb, '{}'::jsonb), NOW()
		)
		RETURNING `+measurementCols,
		id, sampleID, sample.BatchBusinessID, parameter, strings.TrimSpace(in.Value),
		parseMeasurementValue(in.Value), strings.TrimSpace(in.Unit), strings.TrimSpace(in.SpecLimit),
		strings.TrimSpace(in.MethodID), strings.TrimSpace(in.Analyst), strings.TrimSpace(in.Result),
		status, in.Notes, in.Attachments, strings.TrimSpace(in.ReportedAt), attrsOptional(in.Attrs),
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
	var attrs []byte
	if err := rows.Scan(&item.BusinessID, &item.SampleBusinessID, &item.BatchBusinessID,
		&item.Parameter, &item.ValueText, &item.ValueNum, &item.Unit, &item.SpecLimit,
		&item.MethodID, &item.Analyst, &item.Result, &item.Status, &item.Notes,
		&item.Attachments, &item.ReportedAt, &attrs, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return LabMeasurement{}, err
	}
	item.Attrs = attrsMap(attrs)
	return item, nil
}
