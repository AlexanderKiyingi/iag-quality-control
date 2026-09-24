-- 013: Parameterised lab measurements
--
-- The Lab app's Results screen collects (parameter, value, unit, spec limit,
-- method, analyst, pass/fail) for ANY analyte. The platform had nowhere to put
-- that: qc_physical_tests and qc_chemical_tests each have one fixed column per
-- analyte, and POST /lab/results writes qc_batch_lab_summary, which is a
-- six-metric rollup with one row per batch. So the app sent every result as
-- moisture_pct and read it back labelled "moisture" — a caffeine reading was
-- stored and redisplayed as moisture, silently.
--
-- One row per measurement, keyed to a sample. value_num carries the number when
-- the reading parses as one, and value_text always carries what was typed:
-- plenty of real results are "<0.1" or "pass", and refusing those would push
-- users back to the notes field.
--
-- This does NOT replace the rollup. qc_batch_lab_summary is read by the
-- dashboard, SPC analytics, the CoA PDF and the qc.lab.result_recorded payload,
-- so a measurement whose parameter is one of the six known metrics is mirrored
-- into it (see MeasurementRollupField). Without that, recording results through
-- this table would quietly stop feeding all four.

CREATE TABLE IF NOT EXISTS qc_lab_measurements (
    id                  BIGSERIAL PRIMARY KEY,
    business_id         TEXT NOT NULL UNIQUE,
    sample_business_id  TEXT NOT NULL DEFAULT '',
    batch_business_id   TEXT NOT NULL DEFAULT '',
    parameter           TEXT NOT NULL DEFAULT '',
    value_text          TEXT NOT NULL DEFAULT '',
    value_num           DOUBLE PRECISION,
    unit                TEXT NOT NULL DEFAULT '',
    spec_limit          TEXT NOT NULL DEFAULT '',
    method_id           TEXT NOT NULL DEFAULT '',
    analyst             TEXT NOT NULL DEFAULT '',
    result              TEXT NOT NULL DEFAULT '',
    status              TEXT NOT NULL DEFAULT 'reported',
    notes               TEXT NOT NULL DEFAULT '',
    attachments         TEXT NOT NULL DEFAULT '',
    reported_at         DATE,
    attrs               JSONB NOT NULL DEFAULT '{}',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS qc_lab_measurements_sample_idx
    ON qc_lab_measurements (sample_business_id, reported_at DESC);

CREATE INDEX IF NOT EXISTS qc_lab_measurements_parameter_idx
    ON qc_lab_measurements (parameter, reported_at DESC);
