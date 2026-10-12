-- 023: supplier quality
--
-- Procurement owns the supplier master and decides a supplier's status
-- (approved / conditional / suspended). Quality supplies the evidence:
--
--   qc_incoming_inspections  gains supplier_ref, po_ref and receipt_ref —
--                            procurement's ids for who sent the lot and the
--                            purchase order / goods receipt it came on — so
--                            inspections can be rolled up per supplier.
--   qc_scars                 supplier corrective-action requests: raised from a
--                            non-conformance, answered by the supplier, checked
--                            for effectiveness, closed by e-signature (019).
--   qc_supplier_quality      the last scorecard recommendation QC published
--                            per supplier (qc.supplier.scorecard), so an
--                            unchanged scorecard is not announced again.

ALTER TABLE qc_incoming_inspections
    ADD COLUMN IF NOT EXISTS supplier_ref TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS po_ref       TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS receipt_ref  TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS qc_incoming_inspections_supplier_idx
    ON qc_incoming_inspections (supplier_ref, inspected_at DESC) WHERE supplier_ref <> '';

-- Status runs open → awaiting_response → response_received → verifying →
-- closed; cancelled ends one that should not have been raised. Closing needs
-- the effectiveness check to have found the fix effective.
CREATE TABLE IF NOT EXISTS qc_scars (
    business_id          TEXT PRIMARY KEY,
    supplier_ref         TEXT NOT NULL,
    nc_ref               TEXT NOT NULL DEFAULT '',
    inspection_ref       TEXT NOT NULL DEFAULT '',
    title                TEXT NOT NULL,
    description          TEXT NOT NULL DEFAULT '',
    requested_action     TEXT NOT NULL DEFAULT '',
    severity             TEXT NOT NULL DEFAULT 'major' CHECK (severity IN ('minor', 'major', 'critical')),
    due_date             DATE,
    status               TEXT NOT NULL DEFAULT 'open'
                         CHECK (status IN ('open', 'awaiting_response', 'response_received', 'verifying', 'closed', 'cancelled')),
    supplier_contact     TEXT NOT NULL DEFAULT '',
    supplier_root_cause  TEXT NOT NULL DEFAULT '',
    supplier_action      TEXT NOT NULL DEFAULT '',
    response_received_at TIMESTAMPTZ,
    effectiveness_check  TEXT NOT NULL DEFAULT '',
    effective            BOOLEAN,
    closed_at            TIMESTAMPTZ,
    attrs                JSONB NOT NULL DEFAULT '{}',
    updated_by           TEXT NOT NULL DEFAULT '',
    change_reason        TEXT NOT NULL DEFAULT '',
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS qc_scars_supplier_idx ON qc_scars (supplier_ref, status);

DROP TRIGGER IF EXISTS qc_scars_change_log ON qc_scars;

CREATE TRIGGER qc_scars_change_log
    AFTER INSERT OR UPDATE OR DELETE ON qc_scars
    FOR EACH ROW EXECUTE FUNCTION qc_log_change();

CREATE TABLE IF NOT EXISTS qc_supplier_quality (
    supplier_ref       TEXT PRIMARY KEY,
    recommended_status TEXT NOT NULL,
    acceptance_rate    DOUBLE PRECISION,
    lots               INT NOT NULL DEFAULT 0,
    reasons            JSONB NOT NULL DEFAULT '[]',
    published_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
