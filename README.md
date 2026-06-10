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

## RBAC codenames

Registered at startup (`quality-control` service): `qc.view_samples`, `qc.add_sample`, `qc.record_tests`, `qc.issue_coa`, `qc.approve_certification`, `qc.view_instruments`, `qc.view_compliance`, `qc.view_reports`, `qc.admin.read`, etc. Gateway still requires `platform.access_quality_control`.

## Integration

- **Consumers:** `iag-traceability`, `iag-warehouse`
- **Plan:** [TRACEABILITY_AND_SUPPLIER_PLATFORM.md](../../../docs/planning/TRACEABILITY_AND_SUPPLIER_PLATFORM.md)

Registry: [`subrepos.json`](../../../subrepos.json)
