# Validation pack

This is the evidence that the quality system does what its users require. It's built in the GAMP 5 style: user requirements, then installation, operational and performance qualification.

| Part | Where | What |
|---|---|---|
| User requirements | `requirements.json` | What the system must do. Each requirement names the automated tests that verify it, and the OQ browser scripts that exercise it end to end. |
| Traceability | `go run ./cmd/validation-pack -run <go test -json output>` | The matrix of requirement → tests → result. `TestRequirementsTraceToTests` fails if a requirement points at a test that no longer exists. |
| IQ | below | Installation qualification. |
| OQ | `iag-lab/scripts/oq/*.mjs` and the Go suite | Operational qualification against a running service. |
| PQ | yours | Performance qualification: the lab's own runs on production with real lots, signed off by QA. |

## Running the OQ evidence

```sh
docker run -d --name qc-oq -e POSTGRES_PASSWORD=pg -p 55433:5432 postgres:16-alpine
docker exec qc-oq psql -U postgres -c "CREATE SCHEMA qc"
QC_TEST_DATABASE_URL="postgres://postgres:pg@127.0.0.1:55433/postgres?sslmode=disable" \
  go test -json ./... -count=1 > test-run.json
go run ./cmd/validation-pack -run test-run.json > traceability-$(date +%F).md
```

A database test that skips (because `QC_TEST_DATABASE_URL` is unset) leaves its requirement "incomplete", not verified. `traceability-2026-10-13.md` is the first full run: 21 of 21 requirements verified.

## IQ checklist, per environment

1. The deployed commit is the one reviewed (Railway deployment SHA = merge commit on `main`).
2. `GET /health` → 200, and `GET /ready` → 200 with the database reachable.
3. Every migration in `migrations/` is recorded in `qc.schema_migrations`, with no failure in the boot log (`AUTO_MIGRATE`).
4. JWT verification: `JWKS_URL` resolves, and the boot log shows `jwks bootstrap ok`.
5. A signature token can be issued by iag-authentication for the `iag.quality` audience (`USER_TOKEN_AUDIENCES` includes it).
6. `SERVICE_CLIENT_SECRET` is set, and the permission catalogue registered (no "skipping permissions registration" in the log).
7. In a hardened environment, strict RBAC is on: a token with no permissions is refused.
8. Kafka: `KAFKA_BROKERS` is set, and the outbox drains (no growing backlog in `qc.qc_event_outbox`).
9. Alert routes (`GET /api/v1/alert-routes`) name audiences that the notifications service resolves.
10. File storage for attachments: iag-dms has `S3_*` set or a volume at `FILE_STORAGE_DIR`.

Record the date, the person and the result of each step. The executed checklist, the traceability matrix and the OQ script output together make up the validation report.
