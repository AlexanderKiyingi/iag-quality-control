-- 011: Instrument calibrations and stability studies
--
-- Two Lab app screens had nowhere to write.
--
-- A calibration is an EVENT — who calibrated what, against which reference,
-- with what verdict, and when it is next due. All the platform held was two
-- nullable DATE columns on the qc_instruments register row (last_cal_date,
-- next_cal_date, migration 003), overwritten on every save. That records when
-- the last calibration happened but not that it happened: no performer, no
-- standard, no pass/adjust/fail, and no history at all. The register columns
-- are kept and become a mirror of the latest event rather than being dropped,
-- because two live read paths depend on them — the calibration calendar
-- (internal/store/calendar.go) and OverdueInstrumentsCount
-- (internal/store/compliance.go). Rewriting those to a "latest calibration per
-- instrument" subquery would touch two working queries and make the calendar
-- materially more expensive, for no user-visible gain.
--
-- A stability study ships with a single next_pull_date and no child table. A
-- study really has many pull points, but the app's form has exactly one
-- pullDate and one free-text tests field, so a child table would arrive with no
-- writer and no reader. The follow-up is purely additive when the UI grows:
-- qc_stability_pull_points (study_business_id, pull_day, due_date, tests,
-- status, result), with next_pull_date becoming a mirror of the earliest
-- pending point — the same shape as the calibration mirror below.
--
-- attachments is a real TEXT column here rather than a key inside attrs, which
-- is what 010 did. The app treats it as a flat string field like any other, and
-- a column is exportable and filterable; attrs stays for genuinely unforeseen
-- keys.

CREATE TABLE IF NOT EXISTS qc_instrument_calibrations (
    id               BIGSERIAL PRIMARY KEY,
    business_id      TEXT NOT NULL UNIQUE,
    instrument_id    TEXT NOT NULL DEFAULT '',
    instrument_name  TEXT NOT NULL DEFAULT '',
    serial_ref       TEXT NOT NULL DEFAULT '',
    cal_date         DATE,
    standard_ref     TEXT NOT NULL DEFAULT '',
    performed_by     TEXT NOT NULL DEFAULT '',
    next_due         DATE,
    result           TEXT NOT NULL DEFAULT '',
    status           TEXT NOT NULL DEFAULT 'current',
    notes            TEXT NOT NULL DEFAULT '',
    attachments      TEXT NOT NULL DEFAULT '',
    attrs            JSONB NOT NULL DEFAULT '{}',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- No FK to qc_instruments on purpose. FKs are used sparingly here (only the
-- test tables reference qc_samples), and the app's instrument field is free
-- text — a hard FK would reject perfectly good history for a meter nobody has
-- registered yet. instrument_name carries the label so a calibration against an
-- unregistered instrument still reads correctly.

CREATE INDEX IF NOT EXISTS qc_instrument_calibrations_instrument_idx
    ON qc_instrument_calibrations (instrument_id, cal_date DESC);

CREATE INDEX IF NOT EXISTS qc_instrument_calibrations_due_idx
    ON qc_instrument_calibrations (next_due);

CREATE TABLE IF NOT EXISTS qc_stability_studies (
    business_id        TEXT PRIMARY KEY,
    start_date         DATE,
    product            TEXT NOT NULL DEFAULT '',
    storage_condition  TEXT NOT NULL DEFAULT '',
    duration_days      INT NOT NULL DEFAULT 0,
    next_pull_date     DATE,
    tests_scheduled    TEXT NOT NULL DEFAULT '',
    owner              TEXT NOT NULL DEFAULT '',
    status             TEXT NOT NULL DEFAULT 'planned',
    notes              TEXT NOT NULL DEFAULT '',
    attachments        TEXT NOT NULL DEFAULT '',
    attrs              JSONB NOT NULL DEFAULT '{}',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS qc_stability_studies_status_idx
    ON qc_stability_studies (status, next_pull_date);
