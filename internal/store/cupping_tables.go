package store

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"iag-quality-control/backend/internal/domain"
)

// ErrTableClosed is returned for a sheet on a closed cupping table: closing
// reveals the cup codes, and a sheet scored after that is no longer blind.
var ErrTableClosed = errors.New("this cupping table is closed; its sheets are final")

// CuppingTable is one cupping sitting (migration 020).
type CuppingTable struct {
	BusinessID         string    `json:"business_id"`
	Title              string    `json:"title"`
	ScheduledOn        *string   `json:"scheduled_on,omitempty"`
	Form               string    `json:"form"`
	Blind              bool      `json:"blind"`
	Calibration        bool      `json:"calibration"`
	ReferenceEvaluator string    `json:"reference_evaluator"`
	CupsPerSample      int       `json:"cups_per_sample"`
	Status             string    `json:"status"`
	Notes              string    `json:"notes"`
	UpdatedBy          string    `json:"updated_by"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
	// Counts for a list view.
	Cups   int `json:"cups"`
	Sheets int `json:"sheets"`
}

// TableCup is one sample on a table, as the viewer is allowed to see it. On
// an open blind table SampleBusinessID, BatchBusinessID and MeanScore are
// empty — the cupper sees a code, not a lot.
type TableCup struct {
	CupCode           string   `json:"cup_code"`
	SessionBusinessID string   `json:"session_business_id"`
	SampleBusinessID  string   `json:"sample_business_id,omitempty"`
	BatchBusinessID   string   `json:"batch_business_id,omitempty"`
	Evaluators        []string `json:"evaluators"`
	MeanScore         *float64 `json:"mean_score,omitempty"`
}

// CuppingTableView is a table with its cups.
type CuppingTableView struct {
	CuppingTable
	Revealed bool       `json:"revealed"`
	Cups     []TableCup `json:"cups"`
}

// CreateCuppingTableInput sets up a sitting over the given samples.
type CreateCuppingTableInput struct {
	Title              string
	ScheduledOn        string
	Form               string
	Blind              *bool
	Calibration        bool
	ReferenceEvaluator string
	CupsPerSample      int
	SampleIDs          []string
	Notes              string
	Actor              string
}

const cuppingTableCols = `t.business_id, t.title, t.scheduled_on::text, t.form, t.blind, t.calibration,
	t.reference_evaluator, t.cups_per_sample, t.status, t.notes, t.updated_by, t.created_at, t.updated_at`

func scanCuppingTable(row interface{ Scan(...any) error }, extra ...any) (CuppingTable, error) {
	var t CuppingTable
	err := row.Scan(append([]any{&t.BusinessID, &t.Title, &t.ScheduledOn, &t.Form, &t.Blind, &t.Calibration,
		&t.ReferenceEvaluator, &t.CupsPerSample, &t.Status, &t.Notes, &t.UpdatedBy,
		&t.CreatedAt, &t.UpdatedAt}, extra...)...)
	return t, err
}

/*
CreateCuppingTable sets up a sitting: the table, and one cupping session per
sample, each behind a random three-digit cup code unique on the table.

One transaction, so a table never exists with half its cups. Session ids are
allocated as a block up front — nextBusinessID reads MAX outside the
transaction and would hand every session in the loop the same number.
*/
func (s *Store) CreateCuppingTable(ctx context.Context, in CreateCuppingTableInput) (CuppingTableView, error) {
	form := strings.TrimSpace(in.Form)
	if form == "" {
		form = domain.FormSCACVA
	}
	if form != domain.FormSCACVA && form != domain.FormSCA2004 {
		return CuppingTableView{}, fmt.Errorf("%w: form is sca_cva or sca_2004", ErrBadInput)
	}
	samples := cleanDescriptors(in.SampleIDs) // trims and de-duplicates the same way
	if len(samples) == 0 {
		return CuppingTableView{}, fmt.Errorf("%w: a cupping table needs at least one sample", ErrBadInput)
	}
	if len(samples) > 20 {
		return CuppingTableView{}, fmt.Errorf("%w: at most 20 samples on one table", ErrBadInput)
	}
	if in.Calibration && strings.TrimSpace(in.ReferenceEvaluator) == "" {
		return CuppingTableView{}, fmt.Errorf("%w: a calibration table names its reference cupper", ErrBadInput)
	}
	cups := in.CupsPerSample
	if cups == 0 {
		cups = 5
	}
	blind := true
	if in.Blind != nil {
		blind = *in.Blind
	}
	tableID, err := nextBusinessID(ctx, s.pool, "qc_cupping_tables", "CT")
	if err != nil {
		return CuppingTableView{}, err
	}
	sessionIDs, err := nextBusinessIDs(ctx, s, "qc_cupping_sessions", "CUP", len(samples))
	if err != nil {
		return CuppingTableView{}, err
	}
	codes, err := cupCodes(len(samples))
	if err != nil {
		return CuppingTableView{}, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return CuppingTableView{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
		INSERT INTO qc_cupping_tables (business_id, title, scheduled_on, form, blind, calibration,
		                               reference_evaluator, cups_per_sample, status, notes, updated_by)
		VALUES ($1, $2, COALESCE(NULLIF($3, '')::date, CURRENT_DATE), $4, $5, $6, $7, $8, 'open', $9, $10)`,
		tableID, strings.TrimSpace(in.Title), strings.TrimSpace(in.ScheduledOn), form, blind, in.Calibration,
		strings.TrimSpace(in.ReferenceEvaluator), cups, strings.TrimSpace(in.Notes), strings.TrimSpace(in.Actor),
	); err != nil {
		if strings.Contains(err.Error(), "cups_per_sample") {
			return CuppingTableView{}, fmt.Errorf("%w: cups per sample is 1–10", ErrBadInput)
		}
		return CuppingTableView{}, err
	}
	for i, sampleID := range samples {
		tag, err := tx.Exec(ctx, `
			INSERT INTO qc_cupping_sessions (business_id, sample_business_id, batch_business_id,
			                                 session_date, scorers, status, table_business_id, cup_code, form)
			SELECT $1, business_id, batch_business_id, CURRENT_DATE, '[]'::jsonb, 'open', $2, $3, $4
			FROM qc_samples WHERE business_id = $5`,
			sessionIDs[i], tableID, codes[i], form, sampleID)
		if err != nil {
			return CuppingTableView{}, err
		}
		if tag.RowsAffected() == 0 {
			return CuppingTableView{}, fmt.Errorf("%w: sample %s is not registered", ErrNotFound, sampleID)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return CuppingTableView{}, err
	}
	return s.GetCuppingTable(ctx, tableID, false)
}

// ListCuppingTables lists sittings, newest first, with cup and sheet counts.
func (s *Store) ListCuppingTables(ctx context.Context, status string, limit int) ([]CuppingTable, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+cuppingTableCols+`,
		       (SELECT COUNT(*) FROM qc_cupping_sessions c WHERE c.table_business_id = t.business_id),
		       (SELECT COUNT(*) FROM qc_cupping_scores sc JOIN qc_cupping_sessions c
		          ON c.business_id = sc.session_business_id WHERE c.table_business_id = t.business_id)
		FROM qc_cupping_tables t
		WHERE ($1 = '' OR t.status = $1)
		ORDER BY t.scheduled_on DESC NULLS LAST, t.created_at DESC
		LIMIT $2`, strings.TrimSpace(status), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CuppingTable{}
	for rows.Next() {
		var t CuppingTable
		var cupCount, sheetCount int
		if t, err = scanCuppingTable(rows, &cupCount, &sheetCount); err != nil {
			return nil, err
		}
		t.Cups, t.Sheets = cupCount, sheetCount
		out = append(out, t)
	}
	return out, rows.Err()
}

/*
GetCuppingTable returns a table and its cups. On a blind table that is still
open, cups are shown by code only — no sample, batch or running score — unless
reveal is asked for, which the handler grants only to people who may manage
the table. A closed or non-blind table always shows everything.
*/
func (s *Store) GetCuppingTable(ctx context.Context, id string, reveal bool) (CuppingTableView, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+cuppingTableCols+` FROM qc_cupping_tables t WHERE t.business_id = $1`,
		strings.TrimSpace(id))
	t, err := scanCuppingTable(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return CuppingTableView{}, ErrNotFound
	}
	if err != nil {
		return CuppingTableView{}, err
	}
	view := CuppingTableView{CuppingTable: t, Cups: []TableCup{}}
	view.Revealed = reveal || !t.Blind || t.Status == "closed"
	rows, err := s.pool.Query(ctx, `
		SELECT c.cup_code, c.business_id, c.sample_business_id, c.batch_business_id,
		       COALESCE((SELECT jsonb_agg(sc.evaluator ORDER BY sc.evaluator) FROM qc_cupping_scores sc
		                 WHERE sc.session_business_id = c.business_id), '[]'::jsonb),
		       (SELECT round(avg(sc.total_score)::numeric, 2)::float8 FROM qc_cupping_scores sc
		         WHERE sc.session_business_id = c.business_id)
		FROM qc_cupping_sessions c
		WHERE c.table_business_id = $1
		ORDER BY c.cup_code`, t.BusinessID)
	if err != nil {
		return CuppingTableView{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var cup TableCup
		var evaluators []byte
		if err := rows.Scan(&cup.CupCode, &cup.SessionBusinessID, &cup.SampleBusinessID,
			&cup.BatchBusinessID, &evaluators, &cup.MeanScore); err != nil {
			return CuppingTableView{}, err
		}
		cup.Evaluators = []string{}
		_ = json.Unmarshal(evaluators, &cup.Evaluators)
		view.Sheets += len(cup.Evaluators)
		if !view.Revealed {
			cup.SampleBusinessID, cup.BatchBusinessID, cup.MeanScore = "", "", nil
		}
		view.Cups = append(view.Cups, cup)
	}
	view.CuppingTable.Cups = len(view.Cups)
	return view, rows.Err()
}

// CloseCuppingTable freezes a table: no more sheets, codes revealed.
func (s *Store) CloseCuppingTable(ctx context.Context, id, actor, reason string) (CuppingTableView, error) {
	if strings.TrimSpace(reason) == "" {
		reason = "Cupping table closed"
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE qc_cupping_tables SET status = 'closed', updated_by = $2, change_reason = $3, updated_at = NOW()
		WHERE business_id = $1 AND status <> 'closed'`, strings.TrimSpace(id), strings.TrimSpace(actor), reason)
	if err != nil {
		return CuppingTableView{}, err
	}
	if tag.RowsAffected() == 0 {
		if _, err := s.GetCuppingTable(ctx, id, false); err != nil {
			return CuppingTableView{}, err
		}
		return CuppingTableView{}, ErrTableClosed
	}
	return s.GetCuppingTable(ctx, id, true)
}

// nextBusinessIDs allocates n consecutive ids for rows inserted together.
func nextBusinessIDs(ctx context.Context, s *Store, table, prefix string, n int) ([]string, error) {
	first, err := nextBusinessID(ctx, s.pool, table, prefix)
	if err != nil {
		return nil, err
	}
	// first is PREFIX-YY-NNNN; continue the sequence from it.
	var yy string
	var start int
	if _, err := fmt.Sscanf(first[len(prefix)+1:], "%2s-%d", &yy, &start); err != nil {
		return nil, fmt.Errorf("next ids: %w", err)
	}
	ids := make([]string, n)
	for i := range ids {
		ids[i] = fmt.Sprintf("%s-%s-%04d", prefix, yy, start+i)
	}
	return ids, nil
}

// cupCodes draws n distinct three-digit codes, 100–999, from crypto/rand: a
// guessable sequence would undo the point of a blind table.
func cupCodes(n int) ([]string, error) {
	seen := map[string]bool{}
	out := make([]string, 0, n)
	for len(out) < n {
		v, err := rand.Int(rand.Reader, big.NewInt(900))
		if err != nil {
			return nil, err
		}
		code := fmt.Sprintf("%03d", v.Int64()+100)
		if !seen[code] {
			seen[code] = true
			out = append(out, code)
		}
	}
	return out, nil
}

// SessionForCup resolves a cup code on a table to its session, so a tablet
// scores "cup 417" without ever holding the id that leads to the lot.
func (s *Store) SessionForCup(ctx context.Context, tableID, cupCode string) (string, error) {
	var id string
	err := s.pool.QueryRow(ctx, `
		SELECT business_id FROM qc_cupping_sessions WHERE table_business_id = $1 AND cup_code = $2`,
		strings.TrimSpace(tableID), strings.TrimSpace(cupCode)).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return id, err
}
