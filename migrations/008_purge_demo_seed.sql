-- Purge the demo lab roster (004_technicians.sql) and the placeholder external
-- audit calendar (006_lims_gaps.sql).
--
-- Both tables key on a text business_id and nothing references them by foreign key,
-- so the deletes are unconditional. Rows an operator has added since use their own
-- identifiers and are untouched by these predicates.

-- The migration runner already wraps every file in a single transaction holding an
-- advisory lock, so this file must not open one of its own: a COMMIT here would end
-- that outer transaction early and release the lock mid-run.

DELETE FROM qc_technicians
WHERE business_id IN ('AN', 'DO', 'GM', 'JK', 'RN', 'FB');

DELETE FROM qc_external_audits
WHERE business_id IN ('AUD-2026-001', 'AUD-2026-002', 'AUD-2026-003', 'AUD-2026-004');
