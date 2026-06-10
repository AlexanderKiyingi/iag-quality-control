package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type Instrument struct {
	BusinessID        string     `json:"business_id"`
	Name              string     `json:"name"`
	InstrumentType    string     `json:"instrument_type"`
	Location          string     `json:"location"`
	Status            string     `json:"status"`
	OwnerTech         string     `json:"owner_tech"`
	LastCalDate       *string    `json:"last_cal_date,omitempty"`
	NextCalDate       *string    `json:"next_cal_date,omitempty"`
	Note              string     `json:"note"`
	Samples24h        int        `json:"samples_24h"`
	MESAssetTag       string     `json:"mes_asset_tag,omitempty"`
	LastReadingAt     *time.Time `json:"last_reading_at,omitempty"`
	LastReadingValue  *float64   `json:"last_reading_value,omitempty"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type UpsertInstrumentInput struct {
	BusinessID     string
	Name           string
	InstrumentType string
	Location       string
	Status         string
	OwnerTech      string
	LastCalDate    *string
	NextCalDate    *string
	Note           string
	Samples24h     int
	MESAssetTag    string
}

type InstrumentTelemetryUpdate struct {
	Samples24h       int
	Status           string
	LastReadingAt    *time.Time
	LastReadingValue *float64
}

func (s *Store) ListInstruments(ctx context.Context) ([]Instrument, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT business_id, name, instrument_type, location, status, owner_tech,
		       last_cal_date::text, next_cal_date::text, note, samples_24h,
		       mes_asset_tag, last_reading_at, last_reading_value, updated_at
		FROM qc_instruments ORDER BY name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanInstruments(rows)
}

func (s *Store) GetInstrument(ctx context.Context, businessID string) (Instrument, error) {
	var out Instrument
	err := s.pool.QueryRow(ctx, `
		SELECT business_id, name, instrument_type, location, status, owner_tech,
		       last_cal_date::text, next_cal_date::text, note, samples_24h,
		       mes_asset_tag, last_reading_at, last_reading_value, updated_at
		FROM qc_instruments WHERE business_id = $1`, businessID,
	).Scan(
		&out.BusinessID, &out.Name, &out.InstrumentType, &out.Location, &out.Status, &out.OwnerTech,
		&out.LastCalDate, &out.NextCalDate, &out.Note, &out.Samples24h,
		&out.MESAssetTag, &out.LastReadingAt, &out.LastReadingValue, &out.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Instrument{}, ErrNotFound
	}
	return out, err
}

func (s *Store) UpsertInstrument(ctx context.Context, in UpsertInstrumentInput) (Instrument, error) {
	id := strings.TrimSpace(in.BusinessID)
	if id == "" {
		var err error
		id, err = nextBusinessID(ctx, s.pool, "qc_instruments", "INS")
		if err != nil {
			return Instrument{}, err
		}
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return Instrument{}, ErrBadInput
	}
	status := strings.TrimSpace(in.Status)
	if status == "" {
		status = "online"
	}
	var out Instrument
	err := s.pool.QueryRow(ctx, `
		INSERT INTO qc_instruments (
			business_id, name, instrument_type, location, status, owner_tech,
			last_cal_date, next_cal_date, note, samples_24h, mes_asset_tag, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,NOW())
		ON CONFLICT (business_id) DO UPDATE SET
			name = EXCLUDED.name,
			instrument_type = EXCLUDED.instrument_type,
			location = EXCLUDED.location,
			status = EXCLUDED.status,
			owner_tech = EXCLUDED.owner_tech,
			last_cal_date = EXCLUDED.last_cal_date,
			next_cal_date = EXCLUDED.next_cal_date,
			note = EXCLUDED.note,
			samples_24h = EXCLUDED.samples_24h,
			mes_asset_tag = EXCLUDED.mes_asset_tag,
			updated_at = NOW()
		RETURNING business_id, name, instrument_type, location, status, owner_tech,
		          last_cal_date::text, next_cal_date::text, note, samples_24h,
		          mes_asset_tag, last_reading_at, last_reading_value, updated_at`,
		id, name, strings.TrimSpace(in.InstrumentType), strings.TrimSpace(in.Location), status,
		strings.TrimSpace(in.OwnerTech), in.LastCalDate, in.NextCalDate, strings.TrimSpace(in.Note), in.Samples24h,
		strings.TrimSpace(in.MESAssetTag),
	).Scan(
		&out.BusinessID, &out.Name, &out.InstrumentType, &out.Location, &out.Status, &out.OwnerTech,
		&out.LastCalDate, &out.NextCalDate, &out.Note, &out.Samples24h,
		&out.MESAssetTag, &out.LastReadingAt, &out.LastReadingValue, &out.UpdatedAt,
	)
	return out, err
}

func (s *Store) ListInstrumentsForSync(ctx context.Context) ([]Instrument, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT business_id, name, instrument_type, location, status, owner_tech,
		       last_cal_date::text, next_cal_date::text, note, samples_24h,
		       mes_asset_tag, last_reading_at, last_reading_value, updated_at
		FROM qc_instruments
		WHERE mes_asset_tag <> ''
		ORDER BY name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanInstruments(rows)
}

func (s *Store) ApplyInstrumentTelemetry(ctx context.Context, businessID string, upd InstrumentTelemetryUpdate) error {
	status := strings.TrimSpace(upd.Status)
	_, err := s.pool.Exec(ctx, `
		UPDATE qc_instruments SET
			samples_24h = CASE WHEN $2 > 0 THEN $2 ELSE samples_24h END,
			status = CASE WHEN $3 <> '' THEN $3 ELSE status END,
			last_reading_at = COALESCE($4, last_reading_at),
			last_reading_value = COALESCE($5, last_reading_value),
			updated_at = NOW()
		WHERE business_id = $1`,
		businessID, upd.Samples24h, status, upd.LastReadingAt, upd.LastReadingValue,
	)
	return err
}

func scanInstruments(rows pgx.Rows) ([]Instrument, error) {
	defer rows.Close()
	var out []Instrument
	for rows.Next() {
		var item Instrument
		if err := rows.Scan(
			&item.BusinessID, &item.Name, &item.InstrumentType, &item.Location, &item.Status, &item.OwnerTech,
			&item.LastCalDate, &item.NextCalDate, &item.Note, &item.Samples24h,
			&item.MESAssetTag, &item.LastReadingAt, &item.LastReadingValue, &item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
