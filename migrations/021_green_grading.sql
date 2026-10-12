-- 021: green coffee grading
--
-- qc_physical_tests (002) holds a moisture reading, five screens and two
-- defect totals the user typed. A grader works differently: they count beans
-- of each defect type in a 350 g sample, and the handbook turns those counts
-- into full defects (five floaters are one; four are none), splits primary
-- from secondary, and grades the lot on the result. None of that was recorded,
-- so a "Category 1: 2" could not be checked against the beans behind it.
--
-- One row is one grading of one sample:
--
--   defects     jsonb  defect type → bean count, as counted
--   screens     jsonb  screen → grams retained ("20" … "12", "pan")
--   result      jsonb  what the service worked out: full defects per type,
--                      the screen distribution, the SCA grade and why
--   metrics     jsonb  canonical parameter → number (full_defects,
--                      category1_full_defects, screen_18_plus_pct, moisture…),
--                      which the spec engine judges and SPC charts
--   sca_grade          specialty | premium | exchange | below_standard | off_grade
--   verdict / evaluation  as on the other judged registers (016): export
--                      grades such as "Screen 18" are specs on these metrics,
--                      so the lab sets the limits, not this migration
--
-- A grading is an event: it is not edited, it is voided with a reason and
-- graded again. Editing would re-judge a lot after its NC was raised.

CREATE TABLE IF NOT EXISTS qc_green_gradings (
    business_id        TEXT PRIMARY KEY,
    sample_business_id TEXT NOT NULL REFERENCES qc_samples (business_id),
    batch_business_id  TEXT NOT NULL DEFAULT '',
    sample_grams       DOUBLE PRECISION NOT NULL DEFAULT 350,
    moisture_pct       DOUBLE PRECISION,
    water_activity     DOUBLE PRECISION,
    density_g_l        DOUBLE PRECISION,
    quakers            INT,
    color              TEXT NOT NULL DEFAULT '',
    odor               TEXT NOT NULL DEFAULT '',
    defects            JSONB NOT NULL DEFAULT '{}',
    screens            JSONB NOT NULL DEFAULT '{}',
    result             JSONB NOT NULL DEFAULT '{}',
    metrics            JSONB NOT NULL DEFAULT '{}',
    full_defects       DOUBLE PRECISION NOT NULL DEFAULT 0,
    sca_grade          TEXT NOT NULL DEFAULT '',
    grader             TEXT NOT NULL DEFAULT '',
    stage              TEXT NOT NULL DEFAULT '',
    grade_target       TEXT NOT NULL DEFAULT '',
    verdict            TEXT NOT NULL DEFAULT '',
    evaluation         JSONB NOT NULL DEFAULT '[]',
    status             TEXT NOT NULL DEFAULT 'recorded' CHECK (status IN ('recorded', 'void')),
    notes              TEXT NOT NULL DEFAULT '',
    attrs              JSONB NOT NULL DEFAULT '{}',
    updated_by         TEXT NOT NULL DEFAULT '',
    change_reason      TEXT NOT NULL DEFAULT '',
    graded_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS qc_green_gradings_sample_idx ON qc_green_gradings (sample_business_id);
CREATE INDEX IF NOT EXISTS qc_green_gradings_batch_idx ON qc_green_gradings (batch_business_id, graded_at DESC);

DROP TRIGGER IF EXISTS qc_green_gradings_change_log ON qc_green_gradings;

CREATE TRIGGER qc_green_gradings_change_log
    AFTER INSERT OR UPDATE OR DELETE ON qc_green_gradings
    FOR EACH ROW EXECUTE FUNCTION qc_log_change();
