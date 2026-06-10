CREATE TABLE IF NOT EXISTS qc_samples (
    id              BIGSERIAL PRIMARY KEY,
    business_id     TEXT NOT NULL UNIQUE,
    batch_business_id TEXT NOT NULL,
    sample_type     TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL DEFAULT 'pending',
    priority        TEXT NOT NULL DEFAULT 'normal',
    assigned_tech   TEXT NOT NULL DEFAULT '',
    tests_required  JSONB NOT NULL DEFAULT '[]',
    notes           TEXT NOT NULL DEFAULT '',
    received_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS qc_samples_batch_idx ON qc_samples (batch_business_id);
CREATE INDEX IF NOT EXISTS qc_samples_status_idx ON qc_samples (status);
CREATE INDEX IF NOT EXISTS qc_samples_received_idx ON qc_samples (received_at DESC);

CREATE TABLE IF NOT EXISTS qc_physical_tests (
    id                BIGSERIAL PRIMARY KEY,
    business_id       TEXT NOT NULL UNIQUE,
    sample_business_id TEXT NOT NULL REFERENCES qc_samples (business_id),
    batch_business_id TEXT NOT NULL,
    tech_id           TEXT NOT NULL DEFAULT '',
    moisture_pct      DOUBLE PRECISION,
    screen_18         DOUBLE PRECISION,
    screen_17         DOUBLE PRECISION,
    screen_16         DOUBLE PRECISION,
    screen_15         DOUBLE PRECISION,
    screen_pan        DOUBLE PRECISION,
    bulk_density_g_l  DOUBLE PRECISION,
    defect_cat1       INT NOT NULL DEFAULT 0,
    defect_cat2       INT NOT NULL DEFAULT 0,
    status            TEXT NOT NULL DEFAULT 'pass',
    tested_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS qc_physical_tests_sample_idx ON qc_physical_tests (sample_business_id);
CREATE INDEX IF NOT EXISTS qc_physical_tests_batch_idx ON qc_physical_tests (batch_business_id);

CREATE TABLE IF NOT EXISTS qc_chemical_tests (
    id                BIGSERIAL PRIMARY KEY,
    business_id       TEXT NOT NULL UNIQUE,
    sample_business_id TEXT NOT NULL REFERENCES qc_samples (business_id),
    batch_business_id TEXT NOT NULL,
    tech_id           TEXT NOT NULL DEFAULT '',
    moisture_kf       DOUBLE PRECISION,
    water_activity    DOUBLE PRECISION,
    ph                DOUBLE PRECISION,
    chlorogenic_pct   DOUBLE PRECISION,
    caffeine_pct      DOUBLE PRECISION,
    status            TEXT NOT NULL DEFAULT 'pass',
    tested_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS qc_chemical_tests_sample_idx ON qc_chemical_tests (sample_business_id);
CREATE INDEX IF NOT EXISTS qc_chemical_tests_batch_idx ON qc_chemical_tests (batch_business_id);

CREATE TABLE IF NOT EXISTS qc_cupping_sessions (
    id                BIGSERIAL PRIMARY KEY,
    business_id       TEXT NOT NULL UNIQUE,
    sample_business_id TEXT NOT NULL REFERENCES qc_samples (business_id),
    batch_business_id TEXT NOT NULL,
    session_date      DATE NOT NULL DEFAULT CURRENT_DATE,
    scorers           JSONB NOT NULL DEFAULT '[]',
    fragrance         DOUBLE PRECISION NOT NULL DEFAULT 0,
    flavor            DOUBLE PRECISION NOT NULL DEFAULT 0,
    aftertaste        DOUBLE PRECISION NOT NULL DEFAULT 0,
    acidity           DOUBLE PRECISION NOT NULL DEFAULT 0,
    body              DOUBLE PRECISION NOT NULL DEFAULT 0,
    balance           DOUBLE PRECISION NOT NULL DEFAULT 0,
    uniformity        DOUBLE PRECISION NOT NULL DEFAULT 0,
    cleancup          DOUBLE PRECISION NOT NULL DEFAULT 0,
    sweetness         DOUBLE PRECISION NOT NULL DEFAULT 0,
    overall           DOUBLE PRECISION NOT NULL DEFAULT 0,
    defect_cat1       INT NOT NULL DEFAULT 0,
    defect_cat2       INT NOT NULL DEFAULT 0,
    total_score       DOUBLE PRECISION NOT NULL DEFAULT 0,
    notes             TEXT NOT NULL DEFAULT '',
    status            TEXT NOT NULL DEFAULT 'complete',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS qc_cupping_sessions_sample_idx ON qc_cupping_sessions (sample_business_id);
CREATE INDEX IF NOT EXISTS qc_cupping_sessions_batch_idx ON qc_cupping_sessions (batch_business_id);

CREATE TABLE IF NOT EXISTS qc_batch_lab_summary (
    batch_business_id TEXT PRIMARY KEY,
    moisture          DOUBLE PRECISION,
    water_activity    DOUBLE PRECISION,
    cup_score         DOUBLE PRECISION,
    grade             TEXT NOT NULL DEFAULT '',
    defects           INT,
    tester            TEXT NOT NULL DEFAULT '',
    lab_date          DATE,
    latest_sample_id  TEXT,
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS qc_coa (
    id                BIGSERIAL PRIMARY KEY,
    coa_number        TEXT NOT NULL UNIQUE,
    lot_business_id   TEXT NOT NULL,
    batch_business_id TEXT NOT NULL DEFAULT '',
    document_ref      TEXT NOT NULL DEFAULT '',
    issued_by         TEXT NOT NULL DEFAULT '',
    issued_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    status            TEXT NOT NULL DEFAULT 'active',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS qc_coa_lot_idx ON qc_coa (lot_business_id);
CREATE INDEX IF NOT EXISTS qc_coa_issued_idx ON qc_coa (issued_at DESC);
