# iag-quality-control (Lab & CoA)

Laboratory results and Certificate of Analysis (CoA) for export lots — Kafka events unlock traceability QR publish gates.

| Field | Value |
|-------|-------|
| **Port** | `4004` |
| **Gateway prefix** | `/api/v1/quality-control` |
| **Kafka topic** | `iag.quality` |
| **Remote** | [iag-quality-control](https://github.com/AlexanderKiyingi/iag-quality-control) |

## Role

Captures **lab analysis** per batch and **CoA issuance** per export lot. Does not store batch operational state — **`iag-supply-chain`** owns batches and lots. **`iag-traceability`** consumes `qc.*` events for story composition and blocks QR publish until a CoA is recorded.

## Quick start

```bash
cd services/operations/quality-control
cp config/.env.example .env
go run ./cmd/server
curl http://localhost:4004/health
```

Via gateway (when `UPSTREAM_QUALITY_CONTROL` is set):

```bash
curl http://localhost:8080/api/v1/quality-control/health
```

## API

| Method | Path | Description |
|--------|------|-------------|
| GET | `/health` | Liveness |
| GET | `/ready` | Readiness |
| POST | `/api/v1/lab/results` | Record lab metrics for a batch |
| POST | `/api/v1/coa` | Issue CoA for an export lot |

### `POST /api/v1/lab/results`

```json
{
  "batch_business_id": "BAT-2026-035",
  "moisture": 11.2,
  "cup_score": 86.5,
  "grade": "Specialty",
  "tester": "Lab A"
}
```

### `POST /api/v1/coa`

```json
{
  "lot_business_id": "LOT-EXP-2026-018",
  "coa_number": "COA-2026-0042",
  "document_ref": "s3://qc/coa-0042.pdf"
}
```

## Kafka events

| Event type | When |
|------------|------|
| `qc.lab.result_recorded` | Lab metrics stored for a batch |
| `qc.coa.issued` | CoA issued for an export lot (unlocks QR publish) |

## Integration

- **Consumers:** `iag-traceability` (publish gate, journey “Lab” step)
- **Correlation:** `batch_business_id` for lab; `lot_business_id` for CoA
- **Plan:** [TRACEABILITY_AND_SUPPLIER_PLATFORM.md](../../../docs/planning/TRACEABILITY_AND_SUPPLIER_PLATFORM.md)

Registry: [`subrepos.json`](../../../subrepos.json)
