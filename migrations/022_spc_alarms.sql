-- 022: SPC alarms
--
-- A point outside the control limits was visible only to someone who opened
-- the chart. Now each new reading is tested against the Nelson rules
-- (domain.Rules) for its parameter as it is recorded, and a break is an alarm:
-- a row here and a qc.alert notification, plus — where the lab asks for it —
-- a flag-level non-conformance.
--
--   qc_spc_settings  per parameter: which rules apply, whether alarms are on,
--                    whether a break raises an NC. A parameter with no row
--                    uses the defaults (rules 1,2,3,5,6; alarms on; no NC).
--   qc_spc_alerts    one row per rule a reading broke; acknowledged with a
--                    note. (parameter, ref, rule) is unique, so re-checking a
--                    reading never alarms twice.

CREATE TABLE IF NOT EXISTS qc_spc_settings (
    parameter     TEXT PRIMARY KEY,
    -- The change log files rows by business_id (017).
    business_id   TEXT GENERATED ALWAYS AS (parameter) STORED,
    rules         INT[] NOT NULL DEFAULT '{1,2,3,5,6}',
    alarms        BOOLEAN NOT NULL DEFAULT true,
    raise_nc      BOOLEAN NOT NULL DEFAULT false,
    updated_by    TEXT NOT NULL DEFAULT '',
    change_reason TEXT NOT NULL DEFAULT '',
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS qc_spc_alerts (
    id               BIGSERIAL PRIMARY KEY,
    parameter        TEXT NOT NULL,
    rule             INT NOT NULL,
    ref              TEXT NOT NULL,
    batch_business_id TEXT NOT NULL DEFAULT '',
    value            DOUBLE PRECISION NOT NULL,
    center           DOUBLE PRECISION NOT NULL,
    ucl              DOUBLE PRECISION NOT NULL,
    lcl              DOUBLE PRECISION NOT NULL,
    reading_at       TEXT NOT NULL DEFAULT '',
    non_conformance  TEXT NOT NULL DEFAULT '',
    status           TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'acknowledged')),
    acknowledged_by  TEXT NOT NULL DEFAULT '',
    ack_note         TEXT NOT NULL DEFAULT '',
    acknowledged_at  TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (parameter, ref, rule)
);

CREATE INDEX IF NOT EXISTS qc_spc_alerts_open_idx ON qc_spc_alerts (status, created_at DESC);

DROP TRIGGER IF EXISTS qc_spc_settings_change_log ON qc_spc_settings;

CREATE TRIGGER qc_spc_settings_change_log
    AFTER INSERT OR UPDATE OR DELETE ON qc_spc_settings
    FOR EACH ROW EXECUTE FUNCTION qc_log_change();
