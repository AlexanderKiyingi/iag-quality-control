-- 018: one score sheet per cupping evaluator
--
-- qc_cupping_sessions holds ONE set of attribute scores and a JSON list of
-- scorer names. Whoever entered it averaged the panel in their head, so the
-- service could not say whether four Q-graders agreed or one of them scored a
-- different coffee. Inter-evaluator variance — the check that catches an
-- uncalibrated cupper or a mislabelled cup — had no data to run on.
--
-- One row per evaluator per session. The session row stays exactly as it was
-- and holds the PANEL MEAN, because it is what the dashboard, SPC, the CoA PDF
-- and the qc.lab.result_recorded payload already read; a session recorded the
-- old way (no sheets) keeps working and simply has no variance to show.
--
-- An evaluator scores a session once: re-scoring is a new session, not an
-- overwrite, so the unique key is (session, evaluator).

CREATE TABLE IF NOT EXISTS qc_cupping_scores (
    id                   BIGSERIAL PRIMARY KEY,
    session_business_id  TEXT NOT NULL REFERENCES qc_cupping_sessions (business_id),
    evaluator            TEXT NOT NULL,
    fragrance            DOUBLE PRECISION NOT NULL DEFAULT 0,
    flavor               DOUBLE PRECISION NOT NULL DEFAULT 0,
    aftertaste           DOUBLE PRECISION NOT NULL DEFAULT 0,
    acidity              DOUBLE PRECISION NOT NULL DEFAULT 0,
    body                 DOUBLE PRECISION NOT NULL DEFAULT 0,
    balance              DOUBLE PRECISION NOT NULL DEFAULT 0,
    uniformity           DOUBLE PRECISION NOT NULL DEFAULT 0,
    cleancup             DOUBLE PRECISION NOT NULL DEFAULT 0,
    sweetness            DOUBLE PRECISION NOT NULL DEFAULT 0,
    overall              DOUBLE PRECISION NOT NULL DEFAULT 0,
    defect_cat1          INT NOT NULL DEFAULT 0,
    defect_cat2          INT NOT NULL DEFAULT 0,
    total_score          DOUBLE PRECISION NOT NULL DEFAULT 0,
    notes                TEXT NOT NULL DEFAULT '',
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (session_business_id, evaluator),
    CHECK (evaluator <> '')
);

CREATE INDEX IF NOT EXISTS qc_cupping_scores_evaluator_idx
    ON qc_cupping_scores (evaluator, created_at DESC);

-- Sheets are part of the quality record, so they join the 017 change log.
DROP TRIGGER IF EXISTS qc_cupping_scores_change_log ON qc_cupping_scores;

CREATE TRIGGER qc_cupping_scores_change_log
    AFTER INSERT OR UPDATE OR DELETE ON qc_cupping_scores
    FOR EACH ROW EXECUTE FUNCTION qc_log_change();
