-- 019: electronic signatures, and a reason for every change
--
-- 21 CFR Part 11 / EU Annex 11, the two things an auditor asks for after the
-- audit trail (017):
--
-- 1. Signatures. A release, a CoA, closing an NC or a CAPA and approving a
--    specification are acts a named person is accountable for. Each is now a
--    row here: who (from a signature token iag-authentication issues only
--    after the person re-enters their password, §11.200), what it means
--    ("approved", "issued", "closed"…), why, when they re-authenticated, when
--    they signed, and a SHA-256 of the record as it stood at that moment, so a
--    later change to the record cannot pass for what was signed (§11.70).
--    token_jti is unique: a signature token signs exactly one thing.
--    Append-only, like the change log.
--
-- 2. A reason for every change. The registers below carry change_reason,
--    which every write sets (an update without one is refused by the service);
--    the change-log trigger copies it into qc_change_log.reason and keeps it
--    out of the before→after diff, where it would only repeat itself.
--
-- The trigger body below is 017's, with exactly two changes: change_reason is
-- excluded from the diff alongside updated_at / updated_by, and it is written
-- to the new reason column. Nothing else in it is altered.

CREATE TABLE IF NOT EXISTS qc_signatures (
    id            BIGSERIAL PRIMARY KEY,
    entity        TEXT NOT NULL,
    business_id   TEXT NOT NULL,
    meaning       TEXT NOT NULL
                  CHECK (meaning IN ('approved', 'reviewed', 'issued', 'closed', 'rejected')),
    reason        TEXT NOT NULL DEFAULT '',
    signer_sub    TEXT NOT NULL,
    signer_email  TEXT NOT NULL,
    signer_name   TEXT NOT NULL DEFAULT '',
    auth_time     TIMESTAMPTZ NOT NULL,
    signed_at     TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    record_hash   TEXT NOT NULL,
    token_jti     TEXT NOT NULL UNIQUE,
    CHECK (signer_email <> '' AND record_hash <> '')
);

CREATE INDEX IF NOT EXISTS qc_signatures_record_idx
    ON qc_signatures (entity, business_id, signed_at DESC);

CREATE OR REPLACE FUNCTION qc_append_only() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION '% is append-only (% refused)', TG_TABLE_NAME, TG_OP;
END;
$$;

DROP TRIGGER IF EXISTS qc_signatures_no_edit ON qc_signatures;

CREATE TRIGGER qc_signatures_no_edit
    BEFORE UPDATE OR DELETE ON qc_signatures
    FOR EACH ROW EXECUTE FUNCTION qc_append_only();

DROP TRIGGER IF EXISTS qc_signatures_no_truncate ON qc_signatures;

CREATE TRIGGER qc_signatures_no_truncate
    BEFORE TRUNCATE ON qc_signatures
    FOR EACH STATEMENT EXECUTE FUNCTION qc_append_only();

ALTER TABLE qc_change_log
    ADD COLUMN IF NOT EXISTS reason TEXT NOT NULL DEFAULT '';

DO $$
DECLARE
    t text;
BEGIN
    FOREACH t IN ARRAY ARRAY[
        'qc_specifications', 'qc_samples', 'qc_incoming_inspections', 'qc_in_process_checks',
        'qc_non_conformances', 'qc_capas', 'qc_release_decisions'
    ] LOOP
        EXECUTE format('ALTER TABLE %I ADD COLUMN IF NOT EXISTS change_reason TEXT NOT NULL DEFAULT %L', t, '');
    END LOOP;
END;
$$;

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
            CONTINUE WHEN k IN ('updated_at', 'updated_by', 'change_reason');
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

    INSERT INTO qc_change_log (table_name, business_id, op, changes, actor, reason)
    VALUES (TG_TABLE_NAME, rec_id, TG_OP, diff,
            COALESCE(NULLIF(new_j ->> 'updated_by', ''), NULLIF(new_j ->> 'recorded_by', ''),
                     ''),
            COALESCE(new_j ->> 'change_reason', ''));
    RETURN NULL;
END;
$$;
