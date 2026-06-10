CREATE TABLE IF NOT EXISTS qc_custody_logs (
    id                 BIGSERIAL PRIMARY KEY,
    sample_business_id TEXT NOT NULL,
    action             TEXT NOT NULL,
    actor              TEXT NOT NULL DEFAULT '',
    location           TEXT NOT NULL DEFAULT '',
    notes              TEXT NOT NULL DEFAULT '',
    logged_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS qc_custody_sample_idx ON qc_custody_logs (sample_business_id, logged_at DESC);

CREATE TABLE IF NOT EXISTS qc_external_audits (
    business_id   TEXT PRIMARY KEY,
    audit_type    TEXT NOT NULL,
    body          TEXT NOT NULL DEFAULT '',
    description   TEXT NOT NULL DEFAULT '',
    audit_date    DATE NOT NULL,
    status        TEXT NOT NULL DEFAULT 'scheduled',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS qc_external_audits_date_idx ON qc_external_audits (audit_date);

INSERT INTO qc_external_audits (business_id, audit_type, body, description, audit_date, status) VALUES
    ('AUD-2026-001', 'ISO 17025', 'Bureau Veritas', 'Annual surveillance audit', '2026-05-08', 'scheduled'),
    ('AUD-2026-002', 'Organic', 'NOGAMU', 'Re-certification inspection', '2026-06-15', 'scheduled'),
    ('AUD-2026-003', 'RA 2020', 'Rainforest Alliance', 'Chain of custody audit', '2026-07-20', 'scheduled'),
    ('AUD-2026-004', 'Export License', 'UCDA', 'Annual renewal', '2026-09-05', 'scheduled')
ON CONFLICT (business_id) DO NOTHING;
