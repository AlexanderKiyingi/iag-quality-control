-- 024: AQL sampling on incoming inspections; stability pull points and results
--
-- Incoming inspections had a sample size someone typed and a defect count
-- nothing judged. Now the inspector gives the lot size, the inspection level
-- and the AQL; the service picks the ISO 2859-1 plan (sample size, accept /
-- reject numbers — domain.NewSamplingPlan) and judges the nonconforming count
-- against it, alongside the spec limits (016). A lot over its acceptance
-- number fails, which raises its NC like any failing reading.
--
-- Stability studies (011) had one next_pull_date and a free-text tests field.
--
--   qc_stability_pull_points  one row per timepoint (day 0, 30, 90 …), due on
--                             start_date + day; next_pull_date mirrors the
--                             earliest pending one. reminded_at: a due-pull
--                             reminder is sent once.
--   qc_stability_results      readings at a timepoint; the shelf-life estimate
--                             (domain.EstimateShelfLife, ICH Q1E) regresses
--                             them against the active finished-goods spec.

ALTER TABLE qc_incoming_inspections
    ADD COLUMN IF NOT EXISTS lot_size         INT,
    ADD COLUMN IF NOT EXISTS inspection_level TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS aql              DOUBLE PRECISION,
    ADD COLUMN IF NOT EXISTS accept_number    INT,
    ADD COLUMN IF NOT EXISTS reject_number    INT,
    ADD COLUMN IF NOT EXISTS sampling_note    TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS qc_stability_pull_points (
    id                BIGSERIAL PRIMARY KEY,
    study_business_id TEXT NOT NULL REFERENCES qc_stability_studies (business_id),
    pull_day          INT NOT NULL CHECK (pull_day >= 0),
    due_date          DATE NOT NULL,
    status            TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'tested', 'missed', 'cancelled')),
    tested_at         TIMESTAMPTZ,
    reminded_at       TIMESTAMPTZ,
    notes             TEXT NOT NULL DEFAULT '',
    UNIQUE (study_business_id, pull_day)
);

CREATE INDEX IF NOT EXISTS qc_stability_pull_points_due_idx
    ON qc_stability_pull_points (status, due_date);

CREATE TABLE IF NOT EXISTS qc_stability_results (
    id                BIGSERIAL PRIMARY KEY,
    study_business_id TEXT NOT NULL REFERENCES qc_stability_studies (business_id),
    pull_day          INT NOT NULL,
    parameter         TEXT NOT NULL,
    value             DOUBLE PRECISION NOT NULL,
    unit              TEXT NOT NULL DEFAULT '',
    recorded_by       TEXT NOT NULL DEFAULT '',
    notes             TEXT NOT NULL DEFAULT '',
    recorded_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- The change log files rows by business_id (017).
    business_id       TEXT GENERATED ALWAYS AS (study_business_id) STORED
);

CREATE INDEX IF NOT EXISTS qc_stability_results_study_idx
    ON qc_stability_results (study_business_id, parameter, pull_day);

DROP TRIGGER IF EXISTS qc_stability_results_change_log ON qc_stability_results;

CREATE TRIGGER qc_stability_results_change_log
    AFTER INSERT OR UPDATE OR DELETE ON qc_stability_results
    FOR EACH ROW EXECUTE FUNCTION qc_log_change();
