package store

import (
	"context"
	"strings"
	"time"
)

// InstrumentCalibration is one calibration event (migration 011).
type InstrumentCalibration struct {
	BusinessID     string         `json:"business_id"`
	InstrumentID   string         `json:"instrument_id"`
	InstrumentName string         `json:"instrument_name"`
	SerialRef      string         `json:"serial_ref"`
	CalDate        *string        `json:"cal_date,omitempty"`
	StandardRef    string         `json:"standard_ref"`
	PerformedBy    string         `json:"performed_by"`
	NextDue        *string        `json:"next_due,omitempty"`
	Result         string         `json:"result"`
	Status         string         `json:"status"`
	Notes          string         `json:"notes"`
	Attachments    string         `json:"attachments"`
	Attrs          map[string]any `json:"attrs"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

// CreateCalibrationInput is the write shape. A calibration is an event, so
// there is no upsert and nothing to preserve: every field is sent or defaulted.
type CreateCalibrationInput struct {
	BusinessID     string
	InstrumentID   string
	InstrumentName string
	SerialRef      string
	CalDate        string
	StandardRef    string
	PerformedBy    string
	NextDue        string
	Result         string
	Status         string
	Notes          string
	Attachments    string
	Attrs          map[string]any
}

const calibrationCols = `business_id, instrument_id, instrument_name, serial_ref, cal_date::text,
	standard_ref, performed_by, next_due::text, result, status, notes, attachments, attrs,
	created_at, updated_at`

// normalizeCalResult maps the app's verdict labels onto the stored vocabulary.
// An unrecognised word is kept as-is rather than guessed at: a wrong verdict on
// a calibration certificate is worse than an unfamiliar one.
func normalizeCalResult(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "pass", "passed":
		return "pass"
	case "adjust", "adjusted":
		return "adjust"
	case "fail", "failed":
		return "fail"
	default:
		return strings.TrimSpace(v)
	}
}

// normalizeCalStatus maps the app's status labels onto the stored vocabulary.
func normalizeCalStatus(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "current":
		return "current"
	case "due":
		return "due"
	case "overdue":
		return "overdue"
	case "out of service", "out_of_service", "out-of-service":
		return "out_of_service"
	default:
		return strings.TrimSpace(v)
	}
}

func (s *Store) ListInstrumentCalibrations(ctx context.Context, instrumentID, status string, limit int) ([]InstrumentCalibration, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+calibrationCols+`
		FROM qc_instrument_calibrations
		WHERE ($1 = '' OR instrument_id = $1)
		  AND ($2 = '' OR status = $2)
		ORDER BY cal_date DESC NULLS LAST, created_at DESC
		LIMIT $3`,
		strings.TrimSpace(instrumentID), normalizeCalStatus(status), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []InstrumentCalibration{}
	for rows.Next() {
		item, err := scanCalibration(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) GetInstrumentCalibration(ctx context.Context, businessID string) (InstrumentCalibration, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+calibrationCols+`
		FROM qc_instrument_calibrations WHERE business_id = $1`, strings.TrimSpace(businessID))
	if err != nil {
		return InstrumentCalibration{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		return InstrumentCalibration{}, ErrNotFound
	}
	return scanCalibration(rows)
}

/*
CreateInstrumentCalibration records one calibration and mirrors it onto the
instrument register in the same statement.

The mirror exists because the calibration calendar (calendar.go) and
OverdueInstrumentsCount (compliance.go) both read qc_instruments.next_cal_date.
Keeping those columns in step here means neither query has to change and neither
gets slower.

It is one statement rather than a transaction on purpose: nothing in this
package opens a transaction, a single statement is atomic anyway, and it is one
round trip instead of three.

The `i.last_cal_date <= ins.cal_date` guard makes it latest-event-wins, so
back-filling historic calibrations cannot drag the due date backwards and
silently empty the calendar. A calibration against an instrument that is not in
the register updates no rows and is not an error — the event is still recorded.
*/
func (s *Store) CreateInstrumentCalibration(ctx context.Context, in CreateCalibrationInput) (InstrumentCalibration, error) {
	instrumentID := strings.TrimSpace(in.InstrumentID)
	instrumentName := strings.TrimSpace(in.InstrumentName)
	if instrumentID == "" && instrumentName == "" {
		return InstrumentCalibration{}, ErrBadInput
	}
	id := strings.TrimSpace(in.BusinessID)
	if id == "" {
		var err error
		if id, err = nextBusinessID(ctx, s.pool, "qc_instrument_calibrations", "CAL"); err != nil {
			return InstrumentCalibration{}, err
		}
	}
	status := normalizeCalStatus(in.Status)
	if status == "" {
		status = "current"
	}
	rows, err := s.pool.Query(ctx, `
		WITH ins AS (
			INSERT INTO qc_instrument_calibrations (
				business_id, instrument_id, instrument_name, serial_ref, cal_date,
				standard_ref, performed_by, next_due, result, status, notes, attachments, attrs, updated_at
			) VALUES (
				$1, $2, $3, $4, NULLIF($5::text, '')::date,
				$6, $7, NULLIF($8::text, '')::date, $9, $10, $11, $12,
				COALESCE($13::jsonb, '{}'::jsonb), NOW()
			)
			RETURNING business_id, instrument_id, instrument_name, serial_ref, cal_date,
			          standard_ref, performed_by, next_due, result, status, notes, attachments,
			          attrs, created_at, updated_at
		), mirror AS (
			UPDATE qc_instruments i
			   SET last_cal_date = COALESCE(ins.cal_date, i.last_cal_date),
			       next_cal_date = COALESCE(ins.next_due, i.next_cal_date),
			       updated_at = NOW()
			  FROM ins
			 WHERE i.business_id = ins.instrument_id
			   AND ins.cal_date IS NOT NULL
			   AND (i.last_cal_date IS NULL OR i.last_cal_date <= ins.cal_date)
			RETURNING i.business_id
		)
		SELECT business_id, instrument_id, instrument_name, serial_ref, cal_date::text,
		       standard_ref, performed_by, next_due::text, result, status, notes, attachments,
		       attrs, created_at, updated_at
		FROM ins`,
		id, instrumentID, instrumentName, strings.TrimSpace(in.SerialRef), strings.TrimSpace(in.CalDate),
		strings.TrimSpace(in.StandardRef), strings.TrimSpace(in.PerformedBy), strings.TrimSpace(in.NextDue),
		normalizeCalResult(in.Result), status, in.Notes, in.Attachments, attrsOptional(in.Attrs),
	)
	if err != nil {
		return InstrumentCalibration{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return InstrumentCalibration{}, err
		}
		return InstrumentCalibration{}, ErrNotFound
	}
	return scanCalibration(rows)
}

func scanCalibration(rows interface{ Scan(...any) error }) (InstrumentCalibration, error) {
	var item InstrumentCalibration
	var attrs []byte
	if err := rows.Scan(&item.BusinessID, &item.InstrumentID, &item.InstrumentName, &item.SerialRef,
		&item.CalDate, &item.StandardRef, &item.PerformedBy, &item.NextDue, &item.Result,
		&item.Status, &item.Notes, &item.Attachments, &attrs, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return InstrumentCalibration{}, err
	}
	item.Attrs = attrsMap(attrs)
	return item, nil
}
