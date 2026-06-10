CREATE TABLE IF NOT EXISTS qc_certification_requests (
    id                BIGSERIAL PRIMARY KEY,
    business_id       TEXT NOT NULL UNIQUE,
    batch_business_id TEXT NOT NULL,
    lot_business_id   TEXT NOT NULL DEFAULT '',
    coa_number        TEXT NOT NULL DEFAULT '',
    document_ref      TEXT NOT NULL DEFAULT '',
    status            TEXT NOT NULL DEFAULT 'pending',
    stage             TEXT NOT NULL DEFAULT 'qc_review',
    requested_by      TEXT NOT NULL DEFAULT '',
    requested_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at      TIMESTAMPTZ,
    notes             TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS qc_cert_req_batch_idx ON qc_certification_requests (batch_business_id);
CREATE INDEX IF NOT EXISTS qc_cert_req_stage_idx ON qc_certification_requests (stage);

CREATE TABLE IF NOT EXISTS qc_certification_approvals (
    id                  BIGSERIAL PRIMARY KEY,
    request_business_id TEXT NOT NULL REFERENCES qc_certification_requests (business_id),
    stage               TEXT NOT NULL,
    approver            TEXT NOT NULL DEFAULT '',
    decision            TEXT NOT NULL DEFAULT 'pending',
    notes               TEXT NOT NULL DEFAULT '',
    decided_at          TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS qc_cert_approval_req_idx ON qc_certification_approvals (request_business_id);

CREATE TABLE IF NOT EXISTS qc_instruments (
    business_id     TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    instrument_type TEXT NOT NULL DEFAULT '',
    location        TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL DEFAULT 'online',
    owner_tech      TEXT NOT NULL DEFAULT '',
    last_cal_date   DATE,
    next_cal_date   DATE,
    note            TEXT NOT NULL DEFAULT '',
    samples_24h     INT NOT NULL DEFAULT 0,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS qc_compliance_logs (
    business_id    TEXT PRIMARY KEY,
    log_type       TEXT NOT NULL DEFAULT 'HACCP',
    ccp            TEXT NOT NULL DEFAULT '',
    logged_date    DATE NOT NULL DEFAULT CURRENT_DATE,
    logged_time    TEXT NOT NULL DEFAULT '',
    tech_id        TEXT NOT NULL DEFAULT '',
    measured_value TEXT NOT NULL DEFAULT '',
    limit_value    TEXT NOT NULL DEFAULT '',
    status         TEXT NOT NULL DEFAULT 'pass',
    action_taken   TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS qc_compliance_logs_date_idx ON qc_compliance_logs (logged_date DESC);

CREATE TABLE IF NOT EXISTS qc_capas (
    business_id       TEXT PRIMARY KEY,
    title             TEXT NOT NULL,
    source_ref        TEXT NOT NULL DEFAULT '',
    status            TEXT NOT NULL DEFAULT 'open',
    priority          TEXT NOT NULL DEFAULT 'medium',
    owner             TEXT NOT NULL DEFAULT '',
    root_cause        TEXT NOT NULL DEFAULT '',
    corrective_action TEXT NOT NULL DEFAULT '',
    opened_at         DATE NOT NULL DEFAULT CURRENT_DATE,
    closed_at         DATE
);

CREATE INDEX IF NOT EXISTS qc_capas_status_idx ON qc_capas (status);
