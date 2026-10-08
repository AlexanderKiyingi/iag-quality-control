-- 014: attrs on qc_samples
--
-- A sample carries things the Lab app's intake form collects and this table has
-- no column for: where it is stored, how much of it there is, the unit, the
-- condition it arrived in, and its chain-of-custody attachments.
--
-- With nowhere to put them, the app packed them into `notes` as a JSON envelope
-- ({"iagExtra":{...},"notes":"..."}) so they would at least survive a round
-- trip. That works and it is why the data is not lost today, but it means every
-- other consumer of this service reads a JSON blob where a technician's handling
-- notes should be — and attachments were dropped entirely, so a file uploaded
-- against a sample had no reference stored anywhere and was orphaned in DMS.
--
-- attrs is the same overflow column the registers added in 010 through 013. The
-- app can stop rewriting notes, and notes goes back to being notes.

ALTER TABLE qc_samples
    ADD COLUMN IF NOT EXISTS attrs JSONB NOT NULL DEFAULT '{}';
