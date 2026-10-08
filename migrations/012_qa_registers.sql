-- 012: Non-conformances, in-process checks, release decisions, hold events
--
-- The four remaining QA screens in the Lab app had no table on the platform.
-- Each gets its own, and the reasons for not reusing something existing are
-- worth recording because in three of the four cases there was a near fit.
--
-- Non-conformances are NOT a reshaped qc_capas. An NC is the finding; a CAPA is
-- the response. Their status vocabularies differ (open / under_investigation /
-- corrective_action / closed / void, against planned / in_progress / verified /
-- closed / overdue), and qc_capas.source_ref is plainly designed to point at
-- exactly this: a CAPA raised against NCR-26-0007 carries that id. No FK, because
-- an NC may be raised on a later screen than the CAPA that references it.
--
-- In-process checks are NOT an extension of qc_compliance_logs, tempting though
-- it is: that table is already parameter/target/actual shaped. But it has no
-- batch and no stage, it is INSERT-only while the IPC screen has an editable
-- status, GET /compliance/logs has no type filter so every IPC row would pollute
-- the HACCP log a compliance officer reads, and calendar.go promotes any row
-- whose log_type matches '%audit%' into the calendar as an audit — so a process
-- stage named "Audit sampling" would silently manufacture calendar entries.
-- target and actual stay TEXT because the form accepts "<= 12.5%", which matches
-- the measured_value / limit_value TEXT precedent in qc_compliance_logs.
--
-- Release decisions reuse the SHAPE of qc_certification_approvals, not the table.
-- That one is an approval-chain child hardwired to the five-stage export
-- certification ladder, keyed to a batch that already has moisture and a cup
-- score, and it terminates in CoA issuance. A release decision is a standalone
-- judgement on a batch.
--
-- Hold events follow qc_custody_logs (migration 006), which is the right template
-- for an append-only log — but batch-scoped rather than sample-scoped, top-level
-- rather than nested under /samples/:id, and carrying a business_id because the
-- form has a required reference field. Rows are never updated, so there is no
-- updated_at.
--
-- Also here: four columns on qc_capas. The CAPA form has collected dueDate, kind,
-- effectiveness and attachments since it was written and silently dropped all
-- four for want of anywhere to put them.

CREATE TABLE IF NOT EXISTS qc_non_conformances (
    business_id  TEXT PRIMARY KEY,
    raised_date  DATE,
    source_ref   TEXT NOT NULL DEFAULT '',
    title        TEXT NOT NULL,
    severity     TEXT NOT NULL DEFAULT 'minor',
    owner        TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL DEFAULT 'open',
    description  TEXT NOT NULL DEFAULT '',
    root_cause   TEXT NOT NULL DEFAULT '',
    attachments  TEXT NOT NULL DEFAULT '',
    attrs        JSONB NOT NULL DEFAULT '{}',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS qc_non_conformances_status_idx
    ON qc_non_conformances (status, raised_date DESC);

CREATE INDEX IF NOT EXISTS qc_non_conformances_source_idx
    ON qc_non_conformances (source_ref);

CREATE TABLE IF NOT EXISTS qc_in_process_checks (
    business_id  TEXT PRIMARY KEY,
    check_date   DATE,
    stage        TEXT NOT NULL DEFAULT '',
    batch_ref    TEXT NOT NULL DEFAULT '',
    parameter    TEXT NOT NULL DEFAULT '',
    target       TEXT NOT NULL DEFAULT '',
    actual       TEXT NOT NULL DEFAULT '',
    checked_by   TEXT NOT NULL DEFAULT '',
    result       TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL DEFAULT 'open',
    notes        TEXT NOT NULL DEFAULT '',
    attrs        JSONB NOT NULL DEFAULT '{}',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS qc_in_process_checks_batch_idx
    ON qc_in_process_checks (batch_ref, check_date DESC);

CREATE INDEX IF NOT EXISTS qc_in_process_checks_status_idx
    ON qc_in_process_checks (status);

CREATE TABLE IF NOT EXISTS qc_release_decisions (
    business_id    TEXT PRIMARY KEY,
    decision_date  DATE,
    batch_ref      TEXT NOT NULL DEFAULT '',
    product        TEXT NOT NULL DEFAULT '',
    check_refs     TEXT NOT NULL DEFAULT '',
    decided_by     TEXT NOT NULL DEFAULT '',
    decision       TEXT NOT NULL DEFAULT '',
    status         TEXT NOT NULL DEFAULT 'draft',
    notes          TEXT NOT NULL DEFAULT '',
    attachments    TEXT NOT NULL DEFAULT '',
    attrs          JSONB NOT NULL DEFAULT '{}',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS qc_release_decisions_batch_idx
    ON qc_release_decisions (batch_ref, decision_date DESC);

CREATE INDEX IF NOT EXISTS qc_release_decisions_decision_idx
    ON qc_release_decisions (decision);

CREATE TABLE IF NOT EXISTS qc_hold_events (
    id             BIGSERIAL PRIMARY KEY,
    business_id    TEXT NOT NULL UNIQUE,
    event_date     DATE,
    batch_ref      TEXT NOT NULL DEFAULT '',
    event          TEXT NOT NULL DEFAULT '',
    reason         TEXT NOT NULL DEFAULT '',
    recorded_by    TEXT NOT NULL DEFAULT '',
    hold_location  TEXT NOT NULL DEFAULT '',
    status         TEXT NOT NULL DEFAULT 'active_hold',
    notes          TEXT NOT NULL DEFAULT '',
    attrs          JSONB NOT NULL DEFAULT '{}',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS qc_hold_events_batch_idx
    ON qc_hold_events (batch_ref, event_date DESC);

ALTER TABLE qc_capas
    ADD COLUMN IF NOT EXISTS due_date      DATE,
    ADD COLUMN IF NOT EXISTS capa_kind     TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS effectiveness TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS attachments   TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS attrs         JSONB NOT NULL DEFAULT '{}';
