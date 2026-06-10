CREATE TABLE IF NOT EXISTS qc_technicians (
    business_id     TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    role            TEXT NOT NULL DEFAULT '',
    level           TEXT NOT NULL DEFAULT '',
    color           TEXT NOT NULL DEFAULT '#0e6b5f',
    certifications  JSONB NOT NULL DEFAULT '[]',
    active          BOOLEAN NOT NULL DEFAULT true,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO qc_technicians (business_id, name, role, level, color, certifications) VALUES
    ('AN', 'Amara Nakato', 'QC Manager', 'Level 3', '#0e6b5f', '["Q-Grader #A8273","SCA CSP","ISO 17025"]'),
    ('DO', 'David Ouko', 'Sensory Analyst', 'Level 3', '#2c5f8e', '["Q-Grader #D7912","SCA Sensory"]'),
    ('GM', 'Grace Mukasa', 'Chem Analyst', 'Level 2', '#d4791f', '["ISO 17025"]'),
    ('JK', 'Joseph Kiprop', 'Lab Technician', 'Level 2', '#7c3aed', '["HACCP Cat 4"]'),
    ('RN', 'Rebecca Namutebi', 'Sample Prep', 'Level 1', '#a32515', '["SOP-Green-01"]'),
    ('FB', 'Francis Bwanika', 'Physical Tests', 'Level 2', '#059669', '["UCDA Grader"]')
ON CONFLICT (business_id) DO NOTHING;
