-- 015: close the form gaps the Lab app still carries
--
-- Three things the app collects and this service had nowhere to keep, plus one
-- register it had no table for at all.
--
-- 1. A cupping session's experimental context. The Lab Trials form records a
--    hypothesis, the variable that changed, the control it was compared
--    against, the control score, a yield figure and worksheet attachments.
--    qc_cupping_sessions has the SCA score sheet and nothing else, so all six
--    read back blank. attrs is the same overflow column every register got in
--    010 through 014.
--
-- 2. A sample's received date. The intake form marks it required and the
--    column has existed since 002, but CreateSample never accepted it: every
--    sample was stamped NOW() regardless. A lab that enters yesterday's intake
--    this morning could not say so.
--
-- 3. Incoming inspections. A supplier lot arriving is checked before it is
--    accepted — sample size, moisture, defect count, inspector, a verdict and
--    a disposition. There was no table, so the app pointed the screen at
--    physical-tests, where the create threw on a sample field the inspection
--    form has never had; nothing could be recorded at all. It is deliberately
--    NOT qc_in_process_checks: that register is per-batch process stages, and
--    sharing one table would mix supplier receipts with production checks in a
--    single list.

ALTER TABLE qc_cupping_sessions
    ADD COLUMN IF NOT EXISTS attrs JSONB NOT NULL DEFAULT '{}';

CREATE TABLE IF NOT EXISTS qc_incoming_inspections (
    business_id   TEXT PRIMARY KEY,
    inspected_at  DATE,
    source_lot    TEXT NOT NULL DEFAULT '',
    item_ref      TEXT NOT NULL DEFAULT '',
    sample_size   INT NOT NULL DEFAULT 0,
    moisture_pct  DOUBLE PRECISION,
    defect_count  INT NOT NULL DEFAULT 0,
    inspector     TEXT NOT NULL DEFAULT '',
    result        TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL DEFAULT 'open',
    notes         TEXT NOT NULL DEFAULT '',
    attachments   TEXT NOT NULL DEFAULT '',
    attrs         JSONB NOT NULL DEFAULT '{}',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS qc_incoming_inspections_lot_idx
    ON qc_incoming_inspections (source_lot, inspected_at DESC);

CREATE INDEX IF NOT EXISTS qc_incoming_inspections_status_idx
    ON qc_incoming_inspections (status, inspected_at DESC);
