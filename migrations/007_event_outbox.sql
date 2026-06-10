-- Transactional event outbox: QC writes domain rows then enqueues the event
-- here; a relay drains rows to Kafka (topic iag.quality) with retry/backoff so
-- CoA and lab-result events are not lost when the broker is briefly unavailable
-- (the traceability QR-publish gate consumes these).
CREATE TABLE IF NOT EXISTS qc_event_outbox (
    id            BIGSERIAL PRIMARY KEY,
    event_type    TEXT        NOT NULL,
    payload       JSONB       NOT NULL DEFAULT '{}',
    attempts      INT         NOT NULL DEFAULT 0,
    last_error    TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    available_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    dispatched_at TIMESTAMPTZ
);

-- Poll index: only undispatched rows that are due.
CREATE INDEX IF NOT EXISTS idx_qc_event_outbox_due
    ON qc_event_outbox (available_at)
    WHERE dispatched_at IS NULL;
