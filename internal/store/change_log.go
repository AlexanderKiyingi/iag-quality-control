package store

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

// ChangeLogEntry is one write to the quality record (migration 017).
//
// For an UPDATE, Changes holds only the columns that changed, each as
// {"old": .., "new": ..}; for an INSERT the new row, for a DELETE the old one.
type ChangeLogEntry struct {
	ID         int64          `json:"id"`
	Entity     string         `json:"entity"`
	Table      string         `json:"table"`
	BusinessID string         `json:"business_id"`
	Op         string         `json:"op"`
	Changes    map[string]any `json:"changes"`
	Actor      string         `json:"actor"`
	ChangedAt  time.Time      `json:"changed_at"`
}

// changeLogEntities maps the names a client uses — the API's own collection
// names — onto the audited tables. Only these can be queried: the endpoint
// must not become a way to read arbitrary table names into a query.
var changeLogEntities = map[string]string{
	"specifications":       "qc_specifications",
	"samples":              "qc_samples",
	"physical-tests":       "qc_physical_tests",
	"chemical-tests":       "qc_chemical_tests",
	"cupping-sessions":     "qc_cupping_sessions",
	"measurements":         "qc_lab_measurements",
	"incoming-inspections": "qc_incoming_inspections",
	"in-process-checks":    "qc_in_process_checks",
	"non-conformances":     "qc_non_conformances",
	"capas":                "qc_capas",
	"release-decisions":    "qc_release_decisions",
	"hold-events":          "qc_hold_events",
	"coa":                  "qc_coa",
	"calibrations":         "qc_instrument_calibrations",
	"compliance-logs":      "qc_compliance_logs",
}

// ChangeLogTable resolves an entity name (or a table name already) to the
// audited table, or "" when it is not one.
func ChangeLogTable(entity string) string {
	e := strings.ToLower(strings.TrimSpace(entity))
	if t, ok := changeLogEntities[e]; ok {
		return t
	}
	for _, t := range changeLogEntities {
		if t == e {
			return t
		}
	}
	return ""
}

func changeLogEntity(table string) string {
	for e, t := range changeLogEntities {
		if t == table {
			return e
		}
	}
	return table
}

// ListChangeLog reads the history of one record, one entity, or everything,
// newest first. entity "" with a business id searches every table — business
// ids carry their own prefix, so a collision is not a practical concern.
func (s *Store) ListChangeLog(ctx context.Context, entity, businessID, actor string, limit int) ([]ChangeLogEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	table := ""
	if strings.TrimSpace(entity) != "" {
		if table = ChangeLogTable(entity); table == "" {
			return nil, ErrBadInput
		}
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, table_name, business_id, op, changes, actor, changed_at
		FROM qc_change_log
		WHERE ($1 = '' OR table_name = $1) AND ($2 = '' OR business_id = $2)
		  AND ($3 = '' OR actor = $3)
		ORDER BY changed_at DESC, id DESC
		LIMIT $4`, table, strings.TrimSpace(businessID), strings.TrimSpace(actor), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChangeLogEntry{}
	for rows.Next() {
		var item ChangeLogEntry
		var changes []byte
		if err := rows.Scan(&item.ID, &item.Table, &item.BusinessID, &item.Op, &changes,
			&item.Actor, &item.ChangedAt); err != nil {
			return nil, err
		}
		item.Entity = changeLogEntity(item.Table)
		item.Changes = map[string]any{}
		_ = json.Unmarshal(changes, &item.Changes)
		out = append(out, item)
	}
	return out, rows.Err()
}
