-- 010: Lab methods and lab requests
--
-- The Lab app's Methods and Requests screens had no table on the platform:
-- a method is the written procedure a test is run to (scope, equipment,
-- acceptance criteria, version) and a request is a piece of work asked of
-- the lab (product, objective, needed-by, the method to use). Both are
-- upserted by business_id like instruments, so a form save is idempotent.

CREATE TABLE IF NOT EXISTS qc_lab_methods (
    business_id         TEXT PRIMARY KEY,
    name                TEXT NOT NULL,
    version             TEXT NOT NULL DEFAULT '1',
    scope               TEXT NOT NULL DEFAULT '',
    equipment           TEXT NOT NULL DEFAULT '',
    procedure           TEXT NOT NULL DEFAULT '',
    acceptance_criteria TEXT NOT NULL DEFAULT '',
    status              TEXT NOT NULL DEFAULT 'draft',
    attrs               JSONB NOT NULL DEFAULT '{}',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS qc_lab_requests (
    business_id  TEXT PRIMARY KEY,
    request_date DATE,
    product      TEXT NOT NULL DEFAULT '',
    requested_by TEXT NOT NULL DEFAULT '',
    priority     TEXT NOT NULL DEFAULT 'normal',
    objective    TEXT NOT NULL DEFAULT '',
    needed_by    DATE,
    method_id    TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL DEFAULT 'open',
    notes        TEXT NOT NULL DEFAULT '',
    attrs        JSONB NOT NULL DEFAULT '{}',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS qc_lab_requests_status_idx ON qc_lab_requests (status, needed_by);
