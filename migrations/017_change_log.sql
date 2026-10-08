-- 017: a before/after change log for the quality record
--
-- qc_api_audit (001) records that somebody POSTed to a path. It does not record
-- what changed: every write here is an upsert, so overwriting a moisture
-- reading, closing a CAPA or flipping a release decision left no trace of the
-- value it replaced. HACCP / SQF expect exactly that trace for manual changes
-- to quality parameters.
--
-- Why triggers and not store code: there are fifteen write paths across these
-- tables and more will come. A trigger cannot be forgotten by the next one.
--
-- Who: the trigger reads the row's own updated_by (016 added it to the judged
-- registers; this migration adds it to CAPAs and release decisions), falling
-- back to recorded_by for the hold log. A session variable cannot carry the
-- actor here — the store has no transactions, so a SET LOCAL in one statement
-- does not survive to the next. Rows written through a path that does not set
-- updated_by yet are logged with an empty actor; qc_api_audit still has the
-- request for those.
--
-- What: one row per write. INSERT stores the new row, DELETE the old one, and
-- UPDATE only the columns that changed as {"col": {"old": .., "new": ..}}. An
-- UPDATE that changes nothing but the bookkeeping columns logs nothing.
--
-- Append-only: a change log that can be edited is not one. UPDATE, DELETE and
-- TRUNCATE on it raise.

CREATE TABLE IF NOT EXISTS qc_change_log (
    id           BIGSERIAL PRIMARY KEY,
    table_name   TEXT NOT NULL,
    business_id  TEXT NOT NULL DEFAULT '',
    op           TEXT NOT NULL CHECK (op IN ('INSERT', 'UPDATE', 'DELETE')),
    changes      JSONB NOT NULL DEFAULT '{}',
    actor        TEXT NOT NULL DEFAULT '',
    changed_at   TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);

CREATE INDEX IF NOT EXISTS qc_change_log_record_idx
    ON qc_change_log (table_name, business_id, changed_at DESC);

CREATE INDEX IF NOT EXISTS qc_change_log_changed_idx
    ON qc_change_log (changed_at DESC);

ALTER TABLE qc_capas
    ADD COLUMN IF NOT EXISTS updated_by TEXT NOT NULL DEFAULT '';

ALTER TABLE qc_release_decisions
    ADD COLUMN IF NOT EXISTS updated_by TEXT NOT NULL DEFAULT '';

CREATE OR REPLACE FUNCTION qc_log_change() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    old_j  jsonb;
    new_j  jsonb;
    diff   jsonb := '{}'::jsonb;
    k      text;
    rec_id text;
BEGIN
    IF TG_OP IN ('UPDATE', 'DELETE') THEN
        old_j := to_jsonb(OLD);
    END IF;
    IF TG_OP IN ('INSERT', 'UPDATE') THEN
        new_j := to_jsonb(NEW);
    END IF;

    IF TG_OP = 'UPDATE' THEN
        FOR k IN SELECT jsonb_object_keys(new_j) LOOP
            CONTINUE WHEN k IN ('updated_at', 'updated_by');
            IF (old_j -> k) IS DISTINCT FROM (new_j -> k) THEN
                diff := diff || jsonb_build_object(k, jsonb_build_object('old', old_j -> k, 'new', new_j -> k));
            END IF;
        END LOOP;
        IF diff = '{}'::jsonb THEN
            RETURN NULL;
        END IF;
    ELSIF TG_OP = 'INSERT' THEN
        diff := new_j;
    ELSE
        diff := old_j;
    END IF;

    -- Most tables key on business_id; the CoA on coa_number; a cupping sheet
    -- (018) is filed under its session.
    rec_id := COALESCE(new_j ->> 'business_id', old_j ->> 'business_id',
                       new_j ->> 'coa_number', old_j ->> 'coa_number',
                       new_j ->> 'session_business_id', old_j ->> 'session_business_id', '');

    INSERT INTO qc_change_log (table_name, business_id, op, changes, actor)
    VALUES (TG_TABLE_NAME, rec_id, TG_OP, diff,
            COALESCE(NULLIF(new_j ->> 'updated_by', ''), NULLIF(new_j ->> 'recorded_by', ''),
                     ''));
    RETURN NULL;
END;
$$;

CREATE OR REPLACE FUNCTION qc_change_log_immutable() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'qc_change_log is append-only (% refused)', TG_OP;
END;
$$;

DROP TRIGGER IF EXISTS qc_change_log_no_edit ON qc_change_log;

CREATE TRIGGER qc_change_log_no_edit
    BEFORE UPDATE OR DELETE ON qc_change_log
    FOR EACH ROW EXECUTE FUNCTION qc_change_log_immutable();

DROP TRIGGER IF EXISTS qc_change_log_no_truncate ON qc_change_log;

CREATE TRIGGER qc_change_log_no_truncate
    BEFORE TRUNCATE ON qc_change_log
    FOR EACH STATEMENT EXECUTE FUNCTION qc_change_log_immutable();

-- The quality record. The batch rollup (qc_batch_lab_summary) is derived from
-- these and is left out; the outbox, audit and technician registers are not
-- quality data.
DO $$
DECLARE
    t text;
BEGIN
    FOREACH t IN ARRAY ARRAY[
        'qc_specifications', 'qc_samples', 'qc_physical_tests', 'qc_chemical_tests',
        'qc_cupping_sessions', 'qc_lab_measurements', 'qc_incoming_inspections',
        'qc_in_process_checks', 'qc_non_conformances', 'qc_capas', 'qc_release_decisions',
        'qc_hold_events', 'qc_coa', 'qc_instrument_calibrations', 'qc_compliance_logs'
    ] LOOP
        IF to_regclass(t) IS NOT NULL THEN
            EXECUTE format('DROP TRIGGER IF EXISTS %I ON %I', t || '_change_log', t);
            EXECUTE format(
                'CREATE TRIGGER %I AFTER INSERT OR UPDATE OR DELETE ON %I '
                'FOR EACH ROW EXECUTE FUNCTION qc_log_change()', t || '_change_log', t);
        END IF;
    END LOOP;
END;
$$;
