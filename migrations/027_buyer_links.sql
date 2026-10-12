-- 027: buyer links
--
-- A buyer asks for the quality behind a lot. Until now that meant emailing a
-- PDF, or giving them an account. A buyer link is a URL QA sends one buyer for
-- one certificate:
--
--   qc_buyer_links       who it is for, which CoA, when it expires (30 days
--                        by default, at most 365), whether it was revoked and
--                        why. The token itself is never stored — only its
--                        SHA-256 — so a database read cannot recreate a link;
--                        QA sees it once, when it is made.
--   qc_buyer_link_views  each time it was opened (page or PDF), with a coarse
--                        network address, so QA can see it was used. Kept
--                        apart from the link so a view is not a change-log
--                        entry.
--
-- What the buyer sees is the certificate as its CoA template prints it, its
-- verdicts and signatures, and the PDF — nothing internal (supplier, batch,
-- NCs, cupping notes). Only a signed ("issued"), active certificate with a
-- template can be shared. Every view checks the link in the database: revoking it takes
-- effect at once.

CREATE TABLE IF NOT EXISTS qc_buyer_links (
    business_id      TEXT PRIMARY KEY,
    token_hash       TEXT NOT NULL UNIQUE,
    coa_number       TEXT NOT NULL REFERENCES qc_coa (coa_number),
    customer_ref     TEXT NOT NULL DEFAULT '',
    recipient_name   TEXT NOT NULL DEFAULT '',
    recipient_email  TEXT NOT NULL DEFAULT '',
    expires_at       TIMESTAMPTZ NOT NULL,
    revoked_at       TIMESTAMPTZ,
    revoked_by       TEXT NOT NULL DEFAULT '',
    created_by       TEXT NOT NULL DEFAULT '',
    updated_by       TEXT NOT NULL DEFAULT '',
    change_reason    TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS qc_buyer_links_coa_idx ON qc_buyer_links (coa_number, created_at DESC);

CREATE TABLE IF NOT EXISTS qc_buyer_link_views (
    id           BIGSERIAL PRIMARY KEY,
    link_id      TEXT NOT NULL REFERENCES qc_buyer_links (business_id),
    viewed_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    what         TEXT NOT NULL DEFAULT 'page' CHECK (what IN ('page', 'pdf')),
    network      TEXT NOT NULL DEFAULT '',
    user_agent   TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS qc_buyer_link_views_link_idx ON qc_buyer_link_views (link_id, viewed_at DESC);

DROP TRIGGER IF EXISTS qc_buyer_links_change_log ON qc_buyer_links;

CREATE TRIGGER qc_buyer_links_change_log
    AFTER INSERT OR UPDATE OR DELETE ON qc_buyer_links
    FOR EACH ROW EXECUTE FUNCTION qc_log_change();
