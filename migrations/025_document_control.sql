-- 025: controlled documents
--
-- SOPs, methods, forms and spec sheets were attachments with no version, no
-- approval and no notion of which one is in force. A controlled document is:
--
--   qc_controlled_documents  the document: number, title, type, owner,
--                            reviewers, how often it must be reviewed
--   qc_document_versions     its versions. draft → in_review → approved (by
--                            e-signature, 019; locks the version) → effective →
--                            superseded / obsolete. Only one version of a
--                            document is effective at a time. The file lives in
--                            iag-dms's attachment store (attachment_id); this
--                            row is its control record.
--   qc_document_acknowledgements  "read and understood": who confirmed which
--                            effective version, when. These feed training
--                            (HR owns training; it reads them).
--
-- A lab method may name its SOP (qc_lab_methods.sop_ref); a measurement made
-- by a method whose SOP has no effective, in-review-date version is refused.

CREATE TABLE IF NOT EXISTS qc_controlled_documents (
    business_id          TEXT PRIMARY KEY,
    title                TEXT NOT NULL,
    doc_type             TEXT NOT NULL DEFAULT 'sop'
                         CHECK (doc_type IN ('sop', 'method', 'form', 'spec_sheet', 'policy', 'work_instruction')),
    owner                TEXT NOT NULL DEFAULT '',
    reviewers            JSONB NOT NULL DEFAULT '[]',
    review_period_months INT NOT NULL DEFAULT 24 CHECK (review_period_months BETWEEN 1 AND 120),
    obsolete             BOOLEAN NOT NULL DEFAULT false,
    attrs                JSONB NOT NULL DEFAULT '{}',
    updated_by           TEXT NOT NULL DEFAULT '',
    change_reason        TEXT NOT NULL DEFAULT '',
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS qc_document_versions (
    doc_business_id   TEXT NOT NULL REFERENCES qc_controlled_documents (business_id),
    version           INT NOT NULL CHECK (version >= 1),
    -- One id per version, for signatures (019) and the change log (017).
    business_id       TEXT GENERATED ALWAYS AS (doc_business_id || '-v' || version) STORED,
    status            TEXT NOT NULL DEFAULT 'draft'
                      CHECK (status IN ('draft', 'in_review', 'approved', 'effective', 'superseded', 'obsolete')),
    change_summary    TEXT NOT NULL DEFAULT '',
    attachment_id     TEXT NOT NULL DEFAULT '',
    filename          TEXT NOT NULL DEFAULT '',
    submitted_by      TEXT NOT NULL DEFAULT '',
    submitted_at      TIMESTAMPTZ,
    effective_date    DATE,
    next_review_date  DATE,
    retired_at        TIMESTAMPTZ,
    updated_by        TEXT NOT NULL DEFAULT '',
    change_reason     TEXT NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (doc_business_id, version)
);

CREATE UNIQUE INDEX IF NOT EXISTS qc_document_versions_business_id_idx ON qc_document_versions (business_id);

CREATE UNIQUE INDEX IF NOT EXISTS qc_document_versions_one_effective_idx
    ON qc_document_versions (doc_business_id) WHERE status = 'effective';

CREATE TABLE IF NOT EXISTS qc_document_acknowledgements (
    id              BIGSERIAL PRIMARY KEY,
    doc_business_id TEXT NOT NULL,
    version         INT NOT NULL,
    user_sub        TEXT NOT NULL DEFAULT '',
    user_email      TEXT NOT NULL,
    user_name       TEXT NOT NULL DEFAULT '',
    acknowledged_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    FOREIGN KEY (doc_business_id, version) REFERENCES qc_document_versions (doc_business_id, version),
    UNIQUE (doc_business_id, version, user_email)
);

ALTER TABLE qc_lab_methods ADD COLUMN IF NOT EXISTS sop_ref TEXT NOT NULL DEFAULT '';

DROP TRIGGER IF EXISTS qc_controlled_documents_change_log ON qc_controlled_documents;

CREATE TRIGGER qc_controlled_documents_change_log
    AFTER INSERT OR UPDATE OR DELETE ON qc_controlled_documents
    FOR EACH ROW EXECUTE FUNCTION qc_log_change();

DROP TRIGGER IF EXISTS qc_document_versions_change_log ON qc_document_versions;

CREATE TRIGGER qc_document_versions_change_log
    AFTER INSERT OR UPDATE OR DELETE ON qc_document_versions
    FOR EACH ROW EXECUTE FUNCTION qc_log_change();
