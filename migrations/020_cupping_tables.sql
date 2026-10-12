-- 020: cupping tables, blind cups, the SCA Coffee Value Assessment
--
-- Until now a cupping session was one sample. A real cupping is a table: a
-- panel tastes several samples in one sitting, usually blind — each sample
-- sits behind a cup code so nobody scores the lot they know. This adds the
-- table, and puts every session (one per sample) on it with its code.
--
--   qc_cupping_tables     one sitting: form, blind, calibration, status
--   qc_cupping_sessions   gains table_business_id + cup_code (unique per table)
--   qc_cupping_scores     gains form, the CVA scores, flavour descriptors,
--                         and client_ref — the id a tablet gives a sheet it
--                         scored offline, so a re-sync records it once
--
-- Forms. 'sca_2004' is the ten-attribute sheet QC has always had.
-- 'sca_cva' is the SCA Coffee Value Assessment (Standard 103/104-2024):
--   descriptive — intensity 0–15 for fragrance, aroma, flavour, aftertaste,
--                 acidity, sweetness, mouthfeel, plus check-all-that-apply
--                 descriptors (kept in descriptors);
--   affective   — 1–9 hedonic for those seven and overall; the score is
--                 0.65625 × Σ(eight hedonic) + 52.75 − 2 × non-uniform cups
--                 − 4 × defective cups (all-9 = 100, all-5 = 79).
-- The CVA detail lives in cva (jsonb); total_score holds the computed score
-- for either form, so the session mean, SPC, the batch rollup and the CoA
-- keep reading one number.
--
-- Calibration tables compare each cupper with a named reference cupper (the
-- head judge) instead of with the panel median.
--
-- Status: planned → open (scoring) → closed (codes revealed, sheets frozen).
-- A closed table refuses new sheets; the codes are what makes it blind, so a
-- cupper's view of an open blind table never names the sample behind a cup.

CREATE TABLE IF NOT EXISTS qc_cupping_tables (
    business_id          TEXT PRIMARY KEY,
    title                TEXT NOT NULL DEFAULT '',
    scheduled_on         DATE,
    form                 TEXT NOT NULL DEFAULT 'sca_2004' CHECK (form IN ('sca_2004', 'sca_cva')),
    blind                BOOLEAN NOT NULL DEFAULT true,
    calibration          BOOLEAN NOT NULL DEFAULT false,
    reference_evaluator  TEXT NOT NULL DEFAULT '',
    cups_per_sample      INT NOT NULL DEFAULT 5 CHECK (cups_per_sample BETWEEN 1 AND 10),
    status               TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('planned', 'open', 'closed')),
    notes                TEXT NOT NULL DEFAULT '',
    attrs                JSONB NOT NULL DEFAULT '{}',
    updated_by           TEXT NOT NULL DEFAULT '',
    change_reason        TEXT NOT NULL DEFAULT '',
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (NOT calibration OR reference_evaluator <> '')
);

CREATE INDEX IF NOT EXISTS qc_cupping_tables_status_idx
    ON qc_cupping_tables (status, scheduled_on DESC);

ALTER TABLE qc_cupping_sessions
    ADD COLUMN IF NOT EXISTS table_business_id TEXT REFERENCES qc_cupping_tables (business_id),
    ADD COLUMN IF NOT EXISTS cup_code          TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS form              TEXT NOT NULL DEFAULT 'sca_2004';

CREATE UNIQUE INDEX IF NOT EXISTS qc_cupping_sessions_table_code_idx
    ON qc_cupping_sessions (table_business_id, cup_code)
    WHERE table_business_id IS NOT NULL;

ALTER TABLE qc_cupping_scores
    ADD COLUMN IF NOT EXISTS form             TEXT NOT NULL DEFAULT 'sca_2004',
    ADD COLUMN IF NOT EXISTS cva              JSONB NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS descriptors      JSONB NOT NULL DEFAULT '[]',
    ADD COLUMN IF NOT EXISTS non_uniform_cups INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS defective_cups   INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS client_ref       TEXT,
    ADD COLUMN IF NOT EXISTS submitted_at     TIMESTAMPTZ;

CREATE UNIQUE INDEX IF NOT EXISTS qc_cupping_scores_client_ref_idx
    ON qc_cupping_scores (client_ref) WHERE client_ref IS NOT NULL;

-- Tables are part of the quality record: they join the change log (017).
DROP TRIGGER IF EXISTS qc_cupping_tables_change_log ON qc_cupping_tables;

CREATE TRIGGER qc_cupping_tables_change_log
    AFTER INSERT OR UPDATE OR DELETE ON qc_cupping_tables
    FOR EACH ROW EXECUTE FUNCTION qc_log_change();
