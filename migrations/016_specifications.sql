-- 016: specification limits, and a verdict the service computes itself
--
-- Until now every pass/fail in this service was whatever the user typed. The
-- one limit the code knew — moisture <= 12.5% — was a Go constant
-- (domain.MoisturePass), qc_lab_measurements.spec_limit is free text nothing
-- reads, and SPC took USL/LSL as query parameters. A reading of 14% moisture
-- marked "pass" was stored as a pass.
--
-- qc_specifications is the limit master. A spec is keyed by:
--
--   stage      incoming | in_process | finished | any. A reading is matched to
--              its own stage first and falls back to 'any'.
--   parameter  the canonical parameter name (see store.CanonicalParameter), so
--              "Moisture %", "moisture_pct" and "moisture" are one spec.
--   grade      '' applies to every grade; a non-empty grade overrides it for
--              readings that carry that grade.
--
-- action_on_fail deliberately mirrors iag-production's prod_ccp_limits:
--
--   none  record the verdict only
--   flag  verdict fail, and a non-conformance is raised automatically
--   hold  as flag, and the batch / lot goes on hold (qc_hold_events +
--         qc.batch.held)
--
-- Superseding a spec means deactivating it and adding another, so only one
-- active spec may hold a (stage, parameter, grade) slot. The change log (017)
-- keeps the history of edits.
--
-- The three registers that judge a reading — measurements, incoming
-- inspections, in-process checks — get the same four columns:
--
--   verdict          pass | fail | no_spec | not_numeric | '' (not evaluated)
--   evaluation       per-parameter detail: the band the reading was judged
--                    against AT THE TIME, so editing a spec later does not
--                    silently re-judge history
--   override_reason  required when the user's own result contradicts the
--                    verdict; the contradiction is kept, not corrected
--   updated_by       who wrote the row last (read by the 017 change log)
--
-- Auto-raised non-conformances carry auto_source = the record that failed. The
-- unique index is what makes re-saving a still-failing record raise nothing
-- new: the insert hits the conflict and does nothing.

CREATE TABLE IF NOT EXISTS qc_specifications (
    business_id     TEXT PRIMARY KEY,
    stage           TEXT NOT NULL DEFAULT 'any'
                    CHECK (stage IN ('incoming', 'in_process', 'finished', 'any')),
    parameter       TEXT NOT NULL,
    grade           TEXT NOT NULL DEFAULT '',
    label           TEXT NOT NULL DEFAULT '',
    unit            TEXT NOT NULL DEFAULT '',
    lsl             DOUBLE PRECISION,
    usl             DOUBLE PRECISION,
    target          DOUBLE PRECISION,
    action_on_fail  TEXT NOT NULL DEFAULT 'flag'
                    CHECK (action_on_fail IN ('none', 'flag', 'hold')),
    active          BOOLEAN NOT NULL DEFAULT true,
    notes           TEXT NOT NULL DEFAULT '',
    attrs           JSONB NOT NULL DEFAULT '{}',
    updated_by      TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (lsl IS NOT NULL OR usl IS NOT NULL),
    CHECK (lsl IS NULL OR usl IS NULL OR lsl <= usl)
);

CREATE UNIQUE INDEX IF NOT EXISTS qc_specifications_active_slot_idx
    ON qc_specifications (stage, parameter, grade) WHERE active;

-- Seed only what the service already enforced or displayed, so nothing is
-- judged against a limit nobody chose. moisture <= 12.5 is domain.MoisturePass;
-- the incoming copy is the one hold, because a wet green lot accepted into the
-- warehouse is the failure the guide's quarantine step exists for. Water
-- activity <= 0.70 is iag-production's DRYING/WATER_ACT band.
INSERT INTO qc_specifications (business_id, stage, parameter, label, unit, lsl, usl, action_on_fail, notes) VALUES
    ('SPEC-SEED-0001', 'any',      'moisture',       'Moisture',                    '%',  NULL, 12.5, 'flag',
     'Seeded from domain.MoisturePass (016).'),
    ('SPEC-SEED-0002', 'incoming', 'moisture',       'Moisture on receipt',         '%',  NULL, 12.5, 'hold',
     'Seeded (016): a lot over 12.5% on arrival is quarantined.'),
    ('SPEC-SEED-0003', 'any',      'water_activity', 'Water activity',              'aw', NULL, 0.70, 'flag',
     'Seeded from iag-production DRYING/WATER_ACT (016).')
ON CONFLICT (business_id) DO NOTHING;

ALTER TABLE qc_lab_measurements
    ADD COLUMN IF NOT EXISTS stage           TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS verdict         TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS evaluation      JSONB NOT NULL DEFAULT '[]',
    ADD COLUMN IF NOT EXISTS override_reason TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS updated_by      TEXT NOT NULL DEFAULT '';

ALTER TABLE qc_incoming_inspections
    ADD COLUMN IF NOT EXISTS verdict         TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS evaluation      JSONB NOT NULL DEFAULT '[]',
    ADD COLUMN IF NOT EXISTS override_reason TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS updated_by      TEXT NOT NULL DEFAULT '';

ALTER TABLE qc_in_process_checks
    ADD COLUMN IF NOT EXISTS verdict         TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS evaluation      JSONB NOT NULL DEFAULT '[]',
    ADD COLUMN IF NOT EXISTS override_reason TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS updated_by      TEXT NOT NULL DEFAULT '';

ALTER TABLE qc_non_conformances
    ADD COLUMN IF NOT EXISTS auto_source TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS updated_by  TEXT NOT NULL DEFAULT '';

CREATE UNIQUE INDEX IF NOT EXISTS qc_non_conformances_auto_source_idx
    ON qc_non_conformances (auto_source) WHERE auto_source <> '';
