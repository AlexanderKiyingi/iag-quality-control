package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"iag-quality-control/backend/internal/domain"
)

// ErrNeedsOverride is returned when a user's result contradicts the verdict
// the service computed and no override reason was given. It is a 422, not a
// 400: the payload is well-formed, it just asserts something the limits deny.
var ErrNeedsOverride = errors.New("result contradicts the specification verdict; an override_reason is required")

// ErrAutoActions wraps a failure to raise the NC/hold AFTER the record itself
// was saved. The record is returned alongside it: the save happened, and
// answering 500 would invite a retry that, for an insert-only register like
// measurements, records the reading twice. Re-saving an upsert register
// retries the auto actions, which are idempotent.
var ErrAutoActions = errors.New("record saved, but raising its non-conformance / hold failed")

// Specification is one limit band (migration 016).
type Specification struct {
	BusinessID   string         `json:"business_id"`
	Stage        string         `json:"stage"`
	Parameter    string         `json:"parameter"`
	Grade        string         `json:"grade"`
	Label        string         `json:"label"`
	Unit         string         `json:"unit"`
	LSL          *float64       `json:"lsl"`
	USL          *float64       `json:"usl"`
	Target       *float64       `json:"target"`
	ActionOnFail string         `json:"action_on_fail"`
	Active       bool           `json:"active"`
	Notes        string         `json:"notes"`
	Attrs        map[string]any `json:"attrs"`
	UpdatedBy    string         `json:"updated_by"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

// UpsertSpecificationInput is the write shape. Parameter is required. The
// three limits are strings so the nil/""/value contract in optional.go can say
// "clear this limit": nil keeps, "" clears, a number sets.
type UpsertSpecificationInput struct {
	BusinessID   string
	Parameter    string
	Stage        *string
	Grade        *string
	Label        *string
	Unit         *string
	LSL          *string
	USL          *string
	Target       *string
	ActionOnFail *string
	Active       *bool
	Notes        *string
	Attrs        map[string]any
	Actor        string
}

/*
CanonicalParameter folds the many spellings of a parameter into the one a spec
is stored under, so "Moisture %", "moisture_pct" and "Moisture" all find the
same limit.

The six metrics the batch rollup knows reuse MeasurementRollupField's aliases —
one alias table, not two that drift. Anything else is lower-cased with runs of
spaces and punctuation collapsed to "_", which is stable enough for "Caffeine
%" and "caffeine" to meet.
*/
func CanonicalParameter(p string) string {
	if field := MeasurementRollupField(p); field != "" {
		return field
	}
	var b strings.Builder
	underscore := false
	for _, r := range strings.ToLower(strings.TrimSpace(p)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			underscore = false
		case r == '%':
			// "Caffeine %" and "Caffeine" are the same parameter.
		default:
			if !underscore && b.Len() > 0 {
				b.WriteByte('_')
				underscore = true
			}
		}
	}
	return strings.TrimSuffix(b.String(), "_")
}

// NormalizeSpecStage maps a stage word onto the stored vocabulary, or "" when
// it is not one.
func NormalizeSpecStage(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "incoming", "receiving", "receipt", "raw", "raw material":
		return "incoming"
	case "in_process", "in-process", "in process", "process":
		return "in_process"
	case "finished", "finished goods", "final", "release":
		return "finished"
	case "any", "all", "":
		return "any"
	default:
		return ""
	}
}

func normalizeSpecAction(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "none", "record", "record only":
		return domain.ActionNone
	case "flag":
		return domain.ActionFlag
	case "hold":
		return domain.ActionHold
	default:
		return ""
	}
}

// optionalLimit validates an optional limit string without collapsing the
// nil/""/value distinction.
func optionalLimit(v *string) (*string, error) {
	t := trimOptional(v)
	if t == nil || *t == "" {
		return t, nil
	}
	if _, err := strconv.ParseFloat(*t, 64); err != nil {
		return nil, fmt.Errorf("%w: %q is not a number", ErrBadInput, *t)
	}
	return t, nil
}

const specCols = `business_id, stage, parameter, grade, label, unit, lsl, usl, target,
	action_on_fail, active, notes, attrs, updated_by, created_at, updated_at`

func (s *Store) ListSpecifications(ctx context.Context, stage, parameter string, includeInactive bool, limit int) ([]Specification, error) {
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	st := ""
	if strings.TrimSpace(stage) != "" {
		st = NormalizeSpecStage(stage)
	}
	param := ""
	if strings.TrimSpace(parameter) != "" {
		param = CanonicalParameter(parameter)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+specCols+`
		FROM qc_specifications
		WHERE ($1 = '' OR stage = $1) AND ($2 = '' OR parameter = $2) AND ($3 OR active)
		ORDER BY parameter, stage, grade, active DESC, updated_at DESC
		LIMIT $4`, st, param, includeInactive, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Specification{}
	for rows.Next() {
		item, err := scanSpecification(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) GetSpecification(ctx context.Context, businessID string) (Specification, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+specCols+` FROM qc_specifications WHERE business_id = $1`,
		strings.TrimSpace(businessID))
	if err != nil {
		return Specification{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return Specification{}, err
		}
		return Specification{}, ErrNotFound
	}
	return scanSpecification(rows)
}

/*
UpsertSpecification creates or edits a spec by business_id, preserving what the
caller did not send.

Two refusals are worth naming. A second ACTIVE spec for a slot already taken is
a 409 — supersede by deactivating the old one, so it is never ambiguous which
band judged a reading. And a band with no limit at all, or LSL above USL, is a
400 from the table's own CHECKs, surfaced rather than a 500.
*/
func (s *Store) UpsertSpecification(ctx context.Context, in UpsertSpecificationInput) (Specification, error) {
	parameter := CanonicalParameter(in.Parameter)
	if parameter == "" {
		return Specification{}, fmt.Errorf("%w: parameter is required", ErrBadInput)
	}
	var stage *string
	if in.Stage != nil {
		st := NormalizeSpecStage(*in.Stage)
		if st == "" {
			return Specification{}, fmt.Errorf("%w: stage must be incoming, in_process, finished or any", ErrBadInput)
		}
		stage = &st
	}
	var action *string
	if a := blankToNil(in.ActionOnFail); a != nil {
		norm := normalizeSpecAction(*a)
		if norm == "" {
			return Specification{}, fmt.Errorf("%w: action_on_fail must be none, flag or hold", ErrBadInput)
		}
		action = &norm
	}
	lsl, err := optionalLimit(in.LSL)
	if err != nil {
		return Specification{}, err
	}
	usl, err := optionalLimit(in.USL)
	if err != nil {
		return Specification{}, err
	}
	target, err := optionalLimit(in.Target)
	if err != nil {
		return Specification{}, err
	}
	id := strings.TrimSpace(in.BusinessID)
	if id == "" {
		if id, err = nextBusinessID(ctx, s.pool, "qc_specifications", "SPEC"); err != nil {
			return Specification{}, err
		}
	}
	rows, err := s.pool.Query(ctx, `
		INSERT INTO qc_specifications (
			business_id, stage, parameter, grade, label, unit, lsl, usl, target,
			action_on_fail, active, notes, attrs, updated_by, updated_at
		) VALUES (
			$1, COALESCE($2::text, 'any'), $3, COALESCE($4::text, ''), COALESCE($5::text, ''),
			COALESCE($6::text, ''), `+limitValue(7, "lsl")+`, `+limitValue(8, "usl")+`,
			`+limitValue(9, "target")+`,
			COALESCE($10::text, 'flag'), COALESCE($11::bool, true), COALESCE($12::text, ''),
			COALESCE($13::jsonb, '{}'::jsonb), $14, NOW()
		)
		ON CONFLICT (business_id) DO UPDATE SET
			stage = COALESCE($2::text, qc_specifications.stage),
			parameter = EXCLUDED.parameter,
			grade = COALESCE($4::text, qc_specifications.grade),
			label = COALESCE($5::text, qc_specifications.label),
			unit = COALESCE($6::text, qc_specifications.unit),
			lsl = EXCLUDED.lsl,
			usl = EXCLUDED.usl,
			target = EXCLUDED.target,
			action_on_fail = COALESCE($10::text, qc_specifications.action_on_fail),
			active = COALESCE($11::bool, qc_specifications.active),
			notes = COALESCE($12::text, qc_specifications.notes),
			attrs = CASE WHEN $13::jsonb IS NULL THEN qc_specifications.attrs
			             ELSE qc_specifications.attrs || $13::jsonb END,
			updated_by = EXCLUDED.updated_by,
			updated_at = NOW()
		RETURNING `+specCols,
		id, stage, parameter, trimOptional(in.Grade), trimOptional(in.Label), trimOptional(in.Unit),
		lsl, usl, target, action, in.Active, in.Notes, attrsOptional(in.Attrs),
		strings.TrimSpace(in.Actor),
	)
	if err != nil {
		return Specification{}, specWriteErr(err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return Specification{}, specWriteErr(err)
		}
		return Specification{}, ErrNotFound
	}
	return scanSpecification(rows)
}

/*
limitValue is the VALUES-side expression for a nullable limit.

The nil/""/value contract cannot be resolved in the UPDATE SET alone here,
because Postgres evaluates the table's CHECKs on the proposed INSERT row BEFORE
ON CONFLICT routes it to the update. A save that only flips `active` would
propose a row with no limits at all and trip "at least one limit" on a spec
that has two. So "not sent" reads the stored value into the proposed row, and
the update takes EXCLUDED as-is.
*/
func limitValue(n int, col string) string {
	return fmt.Sprintf(`CASE WHEN $%[1]d::text IS NULL
		THEN (SELECT %[2]s FROM qc_specifications WHERE business_id = $1)
		ELSE NULLIF($%[1]d::text, '')::double precision END`, n, col)
}

func specWriteErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return fmt.Errorf("%w: an active specification already covers this stage, parameter and grade — deactivate it first", ErrConflict)
		case "23514":
			return fmt.Errorf("%w: a specification needs at least one limit, and LSL may not exceed USL", ErrBadInput)
		}
	}
	return err
}

/*
ResolveSpec finds the active spec that judges a reading, or ErrNotFound.

Most specific wins, stage before grade: a spec for this stage beats an 'any'
spec, and within that a spec for this grade beats the all-grades one. A reading
with no stage ("") can only be judged by an 'any' spec.
*/
func (s *Store) ResolveSpec(ctx context.Context, stage, parameter, grade string) (Specification, error) {
	st := strings.TrimSpace(stage)
	if st != "" {
		st = NormalizeSpecStage(st)
	}
	if st == "" {
		st = "any"
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+specCols+`
		FROM qc_specifications
		WHERE active AND parameter = $1 AND stage IN ($2, 'any') AND grade IN ($3, '')
		ORDER BY (stage <> 'any') DESC, (grade <> '') DESC
		LIMIT 1`, CanonicalParameter(parameter), st, strings.TrimSpace(grade))
	if err != nil {
		return Specification{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return Specification{}, err
		}
		return Specification{}, ErrNotFound
	}
	return scanSpecification(rows)
}

// Reading is one value a register wants judged.
type Reading struct {
	Parameter string
	Raw       string
	Value     *float64
}

// EvaluationEntry records how one reading was judged — and against what, as
// the band stood at that moment, so a later edit to the spec does not quietly
// re-judge history.
type EvaluationEntry struct {
	Parameter string   `json:"parameter"`
	Raw       string   `json:"raw,omitempty"`
	Value     *float64 `json:"value,omitempty"`
	SpecID    string   `json:"spec_id,omitempty"`
	Stage     string   `json:"stage,omitempty"`
	Grade     string   `json:"grade,omitempty"`
	LSL       *float64 `json:"lsl,omitempty"`
	USL       *float64 `json:"usl,omitempty"`
	Unit      string   `json:"unit,omitempty"`
	Action    string   `json:"action,omitempty"`
	Verdict   string   `json:"verdict"`
}

// Evaluation is the judgement of a whole record.
type Evaluation struct {
	Entries []EvaluationEntry
	Verdict string
	// Action is the strongest action among the failing entries, or "".
	Action string
}

// EntriesJSON is what the evaluation column stores.
func (e Evaluation) EntriesJSON() []byte {
	entries := e.Entries
	if entries == nil {
		entries = []EvaluationEntry{}
	}
	b, _ := json.Marshal(entries)
	return b
}

// FailedEntries lists the entries that failed, for an NC's description.
func (e Evaluation) FailedEntries() []EvaluationEntry {
	var out []EvaluationEntry
	for _, en := range e.Entries {
		if en.Verdict == domain.VerdictFail {
			out = append(out, en)
		}
	}
	return out
}

// Evaluate judges readings against the specs for a stage and grade. Readings
// with nothing in them are skipped — an empty field is not a measurement.
func (s *Store) Evaluate(ctx context.Context, stage, grade string, readings []Reading) (Evaluation, error) {
	var ev Evaluation
	var verdicts []string
	for _, r := range readings {
		if strings.TrimSpace(r.Raw) == "" && r.Value == nil {
			continue
		}
		entry := EvaluationEntry{Parameter: CanonicalParameter(r.Parameter), Raw: strings.TrimSpace(r.Raw), Value: r.Value}
		spec, err := s.ResolveSpec(ctx, stage, r.Parameter, grade)
		switch {
		case errors.Is(err, ErrNotFound):
			entry.Verdict = domain.VerdictNoSpec
		case err != nil:
			return Evaluation{}, err
		default:
			entry.SpecID, entry.Stage, entry.Grade = spec.BusinessID, spec.Stage, spec.Grade
			entry.LSL, entry.USL, entry.Unit, entry.Action = spec.LSL, spec.USL, spec.Unit, spec.ActionOnFail
			switch {
			case r.Value == nil:
				entry.Verdict = domain.VerdictNotNumeric
			case domain.WithinBand(*r.Value, spec.LSL, spec.USL):
				entry.Verdict = domain.VerdictPass
			default:
				entry.Verdict = domain.VerdictFail
				if domain.ActionRank(spec.ActionOnFail) > domain.ActionRank(ev.Action) || ev.Action == "" {
					ev.Action = spec.ActionOnFail
				}
			}
		}
		ev.Entries = append(ev.Entries, entry)
		verdicts = append(verdicts, entry.Verdict)
	}
	ev.Verdict = domain.CombineVerdicts(verdicts)
	return ev, nil
}

/*
reconcileResult decides the result a register row is saved with.

  - The caller sent no result (nil): the verdict speaks — pass/fail become the
    register's own words via mapResult. With no definitive verdict the stored
    result is kept (nil back to the upsert).
  - The caller sent a neutral result ("", "pending"): treated the same way, so a
    form that defaults to Pending still picks up the verdict.
  - The caller sent a result that contradicts a definitive verdict: allowed only
    with an override reason, either sent now or already on the row. Without one
    it is ErrNeedsOverride.
  - Anything else is kept as sent.
*/
func reconcileResult(sent *string, verdict, overrideReason string, mapResult func(string) string) (*string, error) {
	definitive := verdict == domain.VerdictPass || verdict == domain.VerdictFail
	if sent == nil || domain.ResultStance(*sent) == 0 {
		if !definitive {
			return sent, nil
		}
		mapped := mapResult(verdict)
		return &mapped, nil
	}
	if definitive && domain.ContradictsVerdict(*sent, verdict) && strings.TrimSpace(overrideReason) == "" {
		return nil, ErrNeedsOverride
	}
	return sent, nil
}

// AutoActions reports what a failing record set in motion. Created is false
// when the NC already existed — a re-save of a record that was already failing.
type AutoActions struct {
	Action           string `json:"action"`
	NonConformanceID string `json:"non_conformance_id,omitempty"`
	HoldEventID      string `json:"hold_event_id,omitempty"`
	HoldRef          string `json:"hold_ref,omitempty"`
	Created          bool   `json:"created"`
}

type autoActionInput struct {
	SourceRef  string
	SourceKind string
	HoldRef    string
	Actor      string
	Evaluation Evaluation
}

/*
raiseAutoActions turns a failing record into a non-conformance and, for a hold
spec, a hold on the batch or lot.

Idempotent per source record: qc_non_conformances.auto_source is unique, so the
NC insert does nothing when this record already raised one, and the hold insert
selects FROM the NC insert — no new NC, no new hold. One statement, because the
store has no transactions, and because two statements could leave an NC whose
hold never happened.

Correcting the record back to pass does NOT release anything. Clearing a hold is
a person's decision, recorded as a release.
*/
func (s *Store) raiseAutoActions(ctx context.Context, in autoActionInput) (*AutoActions, error) {
	ev := in.Evaluation
	if ev.Verdict != domain.VerdictFail || ev.Action == "" || ev.Action == domain.ActionNone {
		return nil, nil
	}
	ncID, err := nextBusinessID(ctx, s.pool, "qc_non_conformances", "NCR")
	if err != nil {
		return nil, err
	}
	holdID, err := nextBusinessID(ctx, s.pool, "qc_hold_events", "HLD")
	if err != nil {
		return nil, err
	}
	failed := ev.FailedEntries()
	parts := make([]string, 0, len(failed))
	for _, en := range failed {
		parts = append(parts, describeFailure(en))
	}
	title := fmt.Sprintf("Out of specification: %s (%s %s)", strings.Join(parts, "; "), in.SourceKind, in.SourceRef)
	severity := "minor"
	if ev.Action == domain.ActionHold {
		severity = "major"
	}
	attrs, _ := json.Marshal(map[string]any{
		"auto":        true,
		"source_kind": in.SourceKind,
		"hold_ref":    in.HoldRef,
		"evaluation":  failed,
	})
	reason := "Out of specification: " + strings.Join(parts, "; ")
	var outNC, outHold *string
	var created bool
	err = s.pool.QueryRow(ctx, `
		WITH nc AS (
			INSERT INTO qc_non_conformances (
				business_id, raised_date, source_ref, title, severity, status, description,
				attrs, auto_source, updated_by, updated_at
			) VALUES ($1, CURRENT_DATE, $2, $3, $4, 'open', $5, $6::jsonb, $2, $7, NOW())
			ON CONFLICT (auto_source) WHERE auto_source <> '' DO NOTHING
			RETURNING business_id
		), hold AS (
			INSERT INTO qc_hold_events (
				business_id, event_date, batch_ref, event, reason, recorded_by, status, notes, attrs
			)
			SELECT $8, CURRENT_DATE, $9, 'placed_on_hold', $10, $7, 'active_hold',
			       'Placed automatically by specification ' || $11::text,
			       jsonb_build_object('auto', true, 'auto_source', $2::text, 'non_conformance', nc.business_id)
			FROM nc
			WHERE $12::bool AND $9::text <> ''
			RETURNING business_id
		)
		SELECT COALESCE((SELECT business_id FROM nc),
		                (SELECT business_id FROM qc_non_conformances WHERE auto_source = $2)),
		       (SELECT business_id FROM hold),
		       EXISTS (SELECT 1 FROM nc)`,
		ncID, in.SourceRef, title, severity, reason, attrs, in.Actor,
		holdID, in.HoldRef, reason, specIDs(failed), ev.Action == domain.ActionHold,
	).Scan(&outNC, &outHold, &created)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAutoActions, err)
	}
	out := &AutoActions{Action: ev.Action, Created: created}
	if outNC != nil {
		out.NonConformanceID = *outNC
	}
	if outHold != nil {
		out.HoldEventID = *outHold
		out.HoldRef = in.HoldRef
	}
	return out, nil
}

func describeFailure(en EvaluationEntry) string {
	val := en.Raw
	if val == "" && en.Value != nil {
		val = strconv.FormatFloat(*en.Value, 'f', -1, 64)
	}
	if en.Unit != "" && en.Unit != "%" {
		val += " " + en.Unit
	} else if en.Unit == "%" && !strings.HasSuffix(val, "%") {
		val += "%"
	}
	var band []string
	if en.LSL != nil {
		band = append(band, "LSL "+strconv.FormatFloat(*en.LSL, 'f', -1, 64))
	}
	if en.USL != nil {
		band = append(band, "USL "+strconv.FormatFloat(*en.USL, 'f', -1, 64))
	}
	return fmt.Sprintf("%s %s (%s)", en.Parameter, val, strings.Join(band, ", "))
}

func specIDs(entries []EvaluationEntry) string {
	ids := make([]string, 0, len(entries))
	for _, en := range entries {
		if en.SpecID != "" {
			ids = append(ids, en.SpecID)
		}
	}
	return strings.Join(ids, ", ")
}

func scanSpecification(rows interface{ Scan(...any) error }) (Specification, error) {
	var item Specification
	var attrs []byte
	if err := rows.Scan(&item.BusinessID, &item.Stage, &item.Parameter, &item.Grade, &item.Label,
		&item.Unit, &item.LSL, &item.USL, &item.Target, &item.ActionOnFail, &item.Active,
		&item.Notes, &attrs, &item.UpdatedBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return Specification{}, err
	}
	item.Attrs = attrsMap(attrs)
	return item, nil
}

func scanEvaluation(b []byte) []EvaluationEntry {
	out := []EvaluationEntry{}
	if len(b) > 0 {
		_ = json.Unmarshal(b, &out)
	}
	return out
}

// attrString reads a string from an attrs map, "" when absent.
func attrString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}
