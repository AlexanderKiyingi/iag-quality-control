# iag-quality-control (Lab & CoA)

Laboratory Information Management System (LIMS) backend for **CUPPA LIMS** — samples, physical/chemical tests, SCA cupping, certification workflow, instruments, compliance, and Certificate of Analysis (CoA). Kafka events unlock traceability QR publish gates.

| Field | Value |
|-------|-------|
| **Port** | `4004` |
| **Gateway prefix** | `/api/v1/quality-control` |
| **Kafka topic** | `iag.quality` |
| **DB schema** | `qc` |
| **UI prototype** | [`CUPPA_LIMS.html`](CUPPA_LIMS.html) |
| **OpenAPI** | [`docs/openapi.yaml`](docs/openapi.yaml) |
| **Remote** | [iag-quality-control](https://github.com/AlexanderKiyingi/iag-quality-control) |

## Role

Owns **lab workflow data** (samples, tests, cupping, certification, CoA, instruments, compliance). Does not own batch/lot master data — **`iag-supply-chain`** owns batches and export lots (optional S2S validation). **`iag-traceability`** consumes `qc.*` events for story composition and blocks QR publish until a CoA is recorded.

## Quick start

```bash
cd services/operations/quality-control
cp .env.example .env
go run ./cmd/server
curl http://localhost:4004/health
```

Open CUPPA LIMS (uses API when `QC_USE_API` is true):

```bash
# Optional: point at gateway
# In browser console before load: window.QC_API_BASE = 'http://localhost:8080/api/v1/quality-control/api/v1';
start CUPPA_LIMS.html
```

## API overview

### Core LIMS (Phase 1)

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/dashboard/summary` | Lab KPIs |
| GET/POST | `/api/v1/samples` | Sample log |
| GET/PATCH | `/api/v1/samples/{id}` | Detail / status |
| POST | `/api/v1/samples/{id}/physical-tests` | Physical test |
| POST | `/api/v1/samples/{id}/chemical-tests` | Chemical test |
| POST | `/api/v1/samples/{id}/cupping` | SCA cupping |
| POST | `/api/v1/lab/results` | Batch lab summary shortcut |
| GET | `/api/v1/batches/{batchId}/lab` | Batch lab panel |
| GET/POST | `/api/v1/coa` | CoA list / issue |

### Certification (Phase 2)

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/certification/pending` | Awaiting approval |
| POST | `/api/v1/certification/requests` | Start workflow |
| GET | `/api/v1/certification/requests/{id}` | Detail + approvals |
| POST | `/api/v1/certification/requests/{id}/approve` | Stage approve/reject |

Stages: `qc_review` → `ops_approval` → `ceo_signoff` → `coa_issued` → `export_ready`. CEO approval issues CoA and emits `qc.coa.issued`.

### Instruments & compliance (Phase 3)

| Method | Path | Description |
|--------|------|-------------|
| GET/POST | `/api/v1/instruments` | Instrument registry |
| GET | `/api/v1/instruments/{id}` | Instrument detail |
| GET/POST | `/api/v1/compliance/logs` | HACCP / ISO CCP logs |
| GET/POST | `/api/v1/compliance/capas` | CAPA tracking |

### Lab registers (Phase 5)

Added for the Lab app's own screens. Methods and requests arrived with migration
010 and were never documented here; calibrations and stability studies with 011.

| Method | Path | Description |
|--------|------|-------------|
| GET/POST | `/api/v1/lab/methods` | Written test procedures (upsert by `business_id`) |
| GET/POST | `/api/v1/lab/requests` | Work asked of the lab (upsert by `business_id`) |
| GET/POST | `/api/v1/calibrations` | Instrument calibration events; `?instrument=&status=&limit=` |
| GET | `/api/v1/calibrations/{id}` | Single calibration |
| GET/POST | `/api/v1/stability-studies` | Stability studies (upsert by `business_id`); `?status=&limit=` |
| GET | `/api/v1/lab/measurements` | Parameterised results; `?sample=&parameter=&limit=` |
| POST | `/api/v1/samples/{id}/measurements` | Record a result for any analyte |

`/lab/measurements` is the one that records an **arbitrary** analyte.
`POST /lab/results` is a six-metric *batch rollup* (it writes
`qc_batch_lab_summary`, one row per batch), and `physical-tests` /
`chemical-tests` have one fixed column per analyte — so before 013 the Lab app
sent every result as `moisture_pct` and read it back labelled "moisture". A
measurement whose parameter is one of the rollup's six is mirrored into it, so
SPC, the dashboard, the CoA PDF and `qc.lab.result_recorded` keep being fed;
anything else is stored as a measurement and nothing more. `value_text` always
keeps what was typed ("<0.1", "pass"), and `value_num` is filled only when it
parses.

A calibration is an event, so there is no update verb and history is read from
the collection filtered by instrument — **not** from a nested route under
`/instruments/{id}`, which would put a second wildcard name in a position that
already has one and make the router panic at startup.

Recording a calibration also mirrors its dates onto the instrument register, so
the calibration calendar and the overdue count keep working off the columns they
already read. Latest event wins, so back-filling history cannot drag a due date
backwards.

### QA registers (Phase 5)

| Method | Path | Description |
|--------|------|-------------|
| GET/POST | `/api/v1/non-conformances` | Non-conformance register; `?status=&severity=&limit=` |
| GET | `/api/v1/non-conformances/{id}` | Single NC |
| GET/POST | `/api/v1/in-process-checks` | In-process quality checks; `?batch=&status=&limit=` |
| GET/POST | `/api/v1/release-decisions` | Batch release decisions; `?batch=&decision=&limit=` |
| GET/POST | `/api/v1/hold-events` | Hold and release log (append-only); `?batch=&status=&limit=` |
| GET/POST | `/api/v1/incoming-inspections` | Supplier lots checked on arrival; `?lot=&status=&limit=` |

A non-conformance is the finding and a CAPA is the response, linked by
`qc_capas.source_ref` carrying the NC's `business_id`. The hold log is
append-only: re-posting an existing `business_id` is **409**, not an update.

`POST /api/v1/release-decisions` emits `qc.batch.released` (released,
conditional) or `qc.batch.held` (hold, reject, rework) — but **only when the
decision is new or has changed**, since the route is an upsert and a batch is
released once. An unrecognised decision emits nothing rather than guessing.

### Specifications, verdicts and auto actions (016)

| Method | Path | Description |
|--------|------|-------------|
| GET/POST | `/api/v1/specifications` | Limit master; `?stage=&parameter=&include_inactive=true` |
| GET | `/api/v1/specifications/{id}` | Single spec |

A spec is LSL/USL (either may be absent) for a **stage** (`incoming`,
`in_process`, `finished`, `any`), a **parameter** (canonicalised, so
`Moisture %` = `moisture_pct` = `moisture`) and an optional **grade**. The most
specific active spec wins, stage before grade. Only one active spec may hold a
slot (**409** otherwise) — supersede by deactivating.

Measurements, incoming inspections and in-process checks are judged on every
save and carry `verdict` (`pass`/`fail`/`no_spec`/`not_numeric`) and
`evaluation` (the band used, as it stood). A blank or `pending` result takes
the verdict; a result that **contradicts** it needs `override_reason`, else
**422** `override_required`.

`action_on_fail` decides what a failure does: `none` records it, `flag` also
raises a non-conformance (`auto_source` = the failing record), `hold` also
places a hold on the batch/lot and emits `qc.batch.held`. Once per source
record — a re-save raises nothing new — and nothing is released automatically.
The write response carries `auto_actions`. Seeded: moisture ≤ 12.5 (flag; hold
on `incoming`), water activity ≤ 0.70 (flag).

### Change log (017)

`GET /api/v1/change-log?entity=incoming-inspections&id=IIN-26-0004&actor=` —
every write to the quality record, by database trigger: the new row on insert,
`{col: {old, new}}` on update, the old row on delete. Append-only (UPDATE /
DELETE / TRUNCATE raise). The actor is the verified token's email.

### Cupping panels (018)

`POST /api/v1/samples/{id}/cupping` accepts `scores: [{evaluator, fragrance, …,
defect_cat1, defect_cat2, notes}]`; the session then stores the **panel mean**.
`GET /api/v1/cupping-sessions/{id}/scores?threshold=2` returns the sheets with
per-attribute spread and each evaluator's deviation from the panel median;
beyond the threshold is an outlier, and a panel with outliers notifies.

`GET /api/v1/analytics/spc?metric=<any parameter>` now charts any measured
parameter and takes its limits from the spec unless `usl`/`lsl` are passed;
`GET /api/v1/analytics/spc/parameters` lists what has data.

### Deleting

`DELETE` exists on exactly five paths, and only while the row is still planning
data nobody has acted on:

| Path | Deletable while status is |
|---|---|
| `/api/v1/lab/methods/{id}` | `draft` |
| `/api/v1/lab/requests/{id}` | `draft`, `open` |
| `/api/v1/stability-studies/{id}` | `planned` |
| `/api/v1/in-process-checks/{id}` | `open` |
| `/api/v1/incoming-inspections/{id}` | `open` |

Anything further on answers **409** naming the alternative: set the row's status
to void or cancelled, which leaves the history intact. Samples, physical and
chemical tests, cupping sessions, measurements, calibrations, CoAs, certification
requests, custody logs, hold events, release decisions, CAPAs, non-conformances,
compliance logs and external audits have **no** DELETE — a LIMS whose results can
be removed is not evidence of anything. The allow-list is pinned by a test.

### SCM context proxies (read-only)

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/batches` | List batches from supply-chain |
| GET | `/api/v1/batches/{id}` | Batch detail from SCM |
| GET | `/api/v1/batches/{id}/pipeline` | SCM batch + QC lab summary + samples |
| GET | `/api/v1/export-lots` | List export lots from SCM |
| GET | `/api/v1/export-lots/{id}` | Export lot + QC CoA if present |
| GET | `/api/v1/farmers` | List farmers from SCM |
| GET | `/api/v1/farmers/{id}` | Farmer detail from SCM |

Requires `UPSTREAM_SUPPLY_CHAIN` and service credentials.

### Technicians, search, exports

| Method | Path | Description |
|--------|------|-------------|
| GET/POST | `/api/v1/technicians` | Lab technician registry |
| GET | `/api/v1/search?q=` | Global search (samples, CoA, instruments, …) |
| GET | `/api/v1/coa/{coaNumber}` | Single CoA lookup |

### Lab lists, queues, custody, audit pack

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/physical-tests` | Lab-wide physical test list |
| GET | `/api/v1/chemical-tests` | Lab-wide chemical test list |
| GET | `/api/v1/cupping-sessions` | Lab-wide cupping session list |
| GET | `/api/v1/samples/{id}/detail` | Sample + tests + cupping + custody + lab summary |
| GET/POST | `/api/v1/samples/{id}/custody` | Chain-of-custody log |
| GET | `/api/v1/queues/instrument` | Physical test work queue |
| GET | `/api/v1/queues/hplc` | Chemical/HPLC work queue |
| GET | `/api/v1/queues/cupping` | Pending cupping queue |
| GET/POST | `/api/v1/external-audits` | External certification audit schedule |
| GET | `/api/v1/reports/audit-pack` | Bundled compliance + CoA + CAPA export |

Sample status `retest` is supported via `PATCH /samples/{id}`.

### PDF, labels, calendar, analytics

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/coa/{coaNumber}/pdf` | CoA PDF (includes batch lab summary) |
| GET | `/api/v1/samples/{id}/cupping/pdf` | SCA cupping form PDF |
| GET | `/api/v1/reports/day-summary/pdf?date=` | Day report PDF |
| GET | `/api/v1/samples/{id}/label?format=json\|zpl\|svg` | Sample barcode label |
| GET | `/api/v1/calendar?from=&to=` | Certification, calibration, audit calendar |
| GET | `/api/v1/analytics/spc?metric=moisture\|cup_score` | SPC series with UCL/LCL |

### MES integration

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/batches/{id}/pipeline` | SCM + QC + **MES runs/CCP** when `UPSTREAM_MES` set |
| POST | `/api/v1/instruments/sync` | Pull telemetry from MES for mapped instruments |

Instruments accept `mes_asset_tag` for auto-sync (background job every `INSTRUMENT_SYNC_INTERVAL`).

### Reports (Phase 4)

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/reports/day-summary?date=YYYY-MM-DD` | Daily lab report |
| GET | `/api/v1/reports/trends?days=7` | Daily trend points |
| GET | `/api/v1/reports/weekly-summary` | Rolling 7-day rollup |
| GET | `/api/v1/reports/export?type=samples` | CSV export |

### Admin

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/admin/audit-logs` | API audit (Bearer) |
| GET | `/api/v1/admin/monitoring/summary` | Monitoring |

## Configuration

| Variable | Description |
|----------|-------------|
| `DATABASE_URL` | Postgres (required) |
| `KAFKA_BROKERS` | Kafka brokers |
| `KAFKA_REQUIRED` | `true` = reject writes if Kafka down (data still saved until publish step) |
| `UPSTREAM_SUPPLY_CHAIN` | SCM base URL for optional validation |
| `UPSTREAM_MES` | MES base URL for production pipeline + instrument telemetry |
| `INSTRUMENT_SYNC_INTERVAL` | Background MES telemetry sync (default `15m`) |
| `AUTO_VALIDATE_BATCH_SCM` | Validate `batch_business_id` on sample create |
| `AUTO_VALIDATE_EXPORT_LOT_SCM` | Validate `lot_business_id` on CoA / certification |
| `SERVICE_CLIENT_*` | Register `qc.*` permissions with iag-authentication |

## Kafka events

| Event | When |
|-------|------|
| `qc.sample.submitted` | Sample registered |
| `qc.lab.result_recorded` | Test or lab summary update |
| `qc.coa.issued` | CoA issued (direct or via certification) |
| `qc.batch.released` | Release decision of `released` or `conditional`, first time or on change |
| `qc.batch.held` | Release decision of `hold`, `reject` or `rework`, first time or on change; or a failure on a `hold` spec (`automatic: true`) |

The two `qc.batch.*` types have **no consumer yet**. Both `iag-warehouse` and
`iag-traceability` ignore unknown event types (`default: return nil`), so they are
inert until a consumer opts in — safe to ship, but not yet integrated.

## RBAC codenames

Registered at startup (`quality-control` service): `qc.view_samples`, `qc.add_sample`, `qc.record_tests`, `qc.issue_coa`, `qc.approve_certification`, `qc.view_instruments`, `qc.view_compliance`, `qc.view_reports`, `qc.admin.read`, `qc.view_release_decisions`, `qc.decide_release`, etc. Gateway still requires `platform.access_quality_control`.

## Integration

- **Consumers:** `iag-traceability`, `iag-warehouse`
- **Plan:** [TRACEABILITY_AND_SUPPLIER_PLATFORM.md](../../../docs/planning/TRACEABILITY_AND_SUPPLIER_PLATFORM.md)

Registry: [`subrepos.json`](../../../subrepos.json)
