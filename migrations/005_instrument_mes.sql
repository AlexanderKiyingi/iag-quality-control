ALTER TABLE qc_instruments
    ADD COLUMN IF NOT EXISTS mes_asset_tag TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS last_reading_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS last_reading_value DOUBLE PRECISION;

CREATE INDEX IF NOT EXISTS qc_instruments_mes_asset_idx ON qc_instruments (mes_asset_tag)
    WHERE mes_asset_tag <> '';
