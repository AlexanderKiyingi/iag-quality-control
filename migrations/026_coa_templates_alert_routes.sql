-- 026: customer CoA templates; alert routing
--
-- A certificate printed the same three lines (moisture, cup score, grade) for
-- every customer. Buyers specify their own: which results, in what order,
-- under what names and units, with or without the specification, and with
-- their own statement.
--
--   qc_coa_templates  one layout: parameters [{parameter, label, unit,
--                     show_spec}], header note, footer statement, whether
--                     signatures print. customer_ref '' with is_default is the
--                     house layout. A CoA is printed with its own template,
--                     else its customer's, else the default, else the legacy
--                     layout.
--   qc_coa            gains customer_ref and template_ref.
--
-- Every alert went to one audience ("approvals.quality"). qc_alert_routes sends
-- each kind of alert where it belongs; the notifications service resolves an
-- audience to people. The seeded rows keep today's behaviour.

CREATE TABLE IF NOT EXISTS qc_coa_templates (
    business_id      TEXT PRIMARY KEY,
    name             TEXT NOT NULL,
    customer_ref     TEXT NOT NULL DEFAULT '',
    is_default       BOOLEAN NOT NULL DEFAULT false,
    title            TEXT NOT NULL DEFAULT 'Certificate of Analysis',
    header_note      TEXT NOT NULL DEFAULT '',
    footer_statement TEXT NOT NULL DEFAULT '',
    parameters       JSONB NOT NULL DEFAULT '[]',
    show_signatures  BOOLEAN NOT NULL DEFAULT true,
    active           BOOLEAN NOT NULL DEFAULT true,
    updated_by       TEXT NOT NULL DEFAULT '',
    change_reason    TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- One active template per customer, and one house default.
CREATE UNIQUE INDEX IF NOT EXISTS qc_coa_templates_customer_idx
    ON qc_coa_templates (customer_ref) WHERE active AND customer_ref <> '';

CREATE UNIQUE INDEX IF NOT EXISTS qc_coa_templates_default_idx
    ON qc_coa_templates (is_default) WHERE active AND is_default;

ALTER TABLE qc_coa
    ADD COLUMN IF NOT EXISTS customer_ref TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS template_ref TEXT NOT NULL DEFAULT '';

DROP TRIGGER IF EXISTS qc_coa_templates_change_log ON qc_coa_templates;

CREATE TRIGGER qc_coa_templates_change_log
    AFTER INSERT OR UPDATE OR DELETE ON qc_coa_templates
    FOR EACH ROW EXECUTE FUNCTION qc_log_change();

CREATE TABLE IF NOT EXISTS qc_alert_routes (
    kind          TEXT PRIMARY KEY,
    business_id   TEXT GENERATED ALWAYS AS (kind) STORED,
    description   TEXT NOT NULL DEFAULT '',
    audience      TEXT NOT NULL DEFAULT 'approvals.quality',
    channel       TEXT NOT NULL DEFAULT '',
    recipient     TEXT NOT NULL DEFAULT '',
    enabled       BOOLEAN NOT NULL DEFAULT true,
    updated_by    TEXT NOT NULL DEFAULT '',
    change_reason TEXT NOT NULL DEFAULT '',
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO qc_alert_routes (kind, description) VALUES
    ('out_of_spec',   'A reading failed its specification (NC / hold raised)'),
    ('spc',           'A control-chart rule was broken'),
    ('cupping',       'A cupping failed, or a panel disagreed'),
    ('stability',     'A stability pull is due'),
    ('certification', 'A certification step needs approval, or was decided'),
    ('supplier',      'Quality''s recommendation for a supplier changed')
ON CONFLICT (kind) DO NOTHING;

DROP TRIGGER IF EXISTS qc_alert_routes_change_log ON qc_alert_routes;

CREATE TRIGGER qc_alert_routes_change_log
    AFTER INSERT OR UPDATE OR DELETE ON qc_alert_routes
    FOR EACH ROW EXECUTE FUNCTION qc_log_change();
