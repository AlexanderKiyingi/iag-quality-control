package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"iag-quality-control/backend/internal/domain"
)

func (s *Store) CreateCupping(ctx context.Context, in CreateCuppingInput) (CuppingSession, error) {
	sample, err := s.GetSample(ctx, in.SampleBusinessID)
	if err != nil {
		return CuppingSession{}, err
	}
	businessID, err := nextBusinessID(ctx, s.pool, "qc_cupping_sessions", "CUP")
	if err != nil {
		return CuppingSession{}, err
	}
	sheets, err := normalizePanel(in.Scores)
	if err != nil {
		return CuppingSession{}, err
	}
	if len(sheets) > 0 {
		in = withPanelMean(in, sheets)
	}
	scores := map[string]float64{
		"fragrance":  in.Fragrance,
		"flavor":     in.Flavor,
		"aftertaste": in.Aftertaste,
		"acidity":    in.Acidity,
		"body":       in.Body,
		"balance":    in.Balance,
		"uniformity": in.Uniformity,
		"cleancup":   in.CleanCup,
		"sweetness":  in.Sweetness,
		"overall":    in.Overall,
	}
	total := domain.CalcSCATotal(scores, in.DefectCat1, in.DefectCat2)
	if len(sheets) > 0 {
		// The panel's score is the mean of the evaluators' totals. Recomputing
		// it from the mean sheet would round the defect counts first.
		total = panelMeanTotal(sheets)
	}
	sheetsJSON, err := json.Marshal(sheets)
	if err != nil {
		return CuppingSession{}, err
	}
	grade := domain.SCATier(total)
	scorers := in.Scorers
	if scorers == nil {
		scorers = []string{}
	}
	scorersJSON, err := json.Marshal(scorers)
	if err != nil {
		return CuppingSession{}, err
	}
	sessionDate := time.Now().UTC().Format("2006-01-02")

	var out CuppingSession
	var cuppingAttrs []byte
	// Session and sheets in one statement: the store has no transactions, and a
	// session whose sheets failed to save would report a panel mean nobody can
	// see the parts of. The sheets' FK to the session is checked at the end of
	// the statement, after the session row exists.
	err = s.pool.QueryRow(ctx, `
		WITH sess AS (
			INSERT INTO qc_cupping_sessions (
				business_id, sample_business_id, batch_business_id, session_date, scorers,
				fragrance, flavor, aftertaste, acidity, body, balance, uniformity, cleancup, sweetness, overall,
				defect_cat1, defect_cat2, total_score, notes, attrs
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,
			          COALESCE($20::jsonb,'{}'::jsonb))
			RETURNING business_id, sample_business_id, batch_business_id, session_date::text, scorers,
			          fragrance, flavor, aftertaste, acidity, body, balance, uniformity, cleancup, sweetness, overall,
			          defect_cat1, defect_cat2, total_score, notes, status, attrs
		), sheets AS (
			INSERT INTO qc_cupping_scores (
				session_business_id, evaluator, fragrance, flavor, aftertaste, acidity, body,
				balance, uniformity, cleancup, sweetness, overall, defect_cat1, defect_cat2,
				total_score, notes
			)
			SELECT (SELECT business_id FROM sess), r.evaluator, r.fragrance, r.flavor, r.aftertaste,
			       r.acidity, r.body, r.balance, r.uniformity, r.cleancup, r.sweetness, r.overall,
			       r.defect_cat1, r.defect_cat2, r.total_score, r.notes
			FROM jsonb_to_recordset($21::jsonb) AS r(
				evaluator text, fragrance float8, flavor float8, aftertaste float8, acidity float8,
				body float8, balance float8, uniformity float8, cleancup float8, sweetness float8,
				overall float8, defect_cat1 int, defect_cat2 int, total_score float8, notes text)
		)
		SELECT * FROM sess`,
		businessID, sample.BusinessID, sample.BatchBusinessID, sessionDate, scorersJSON,
		in.Fragrance, in.Flavor, in.Aftertaste, in.Acidity, in.Body, in.Balance,
		in.Uniformity, in.CleanCup, in.Sweetness, in.Overall,
		in.DefectCat1, in.DefectCat2, total, strings.TrimSpace(in.Notes),
		attrsOptional(in.Attrs), sheetsJSON,
	).Scan(
		&out.BusinessID, &out.SampleBusinessID, &out.BatchBusinessID, &out.SessionDate, &scorersJSON,
		&out.Fragrance, &out.Flavor, &out.Aftertaste, &out.Acidity, &out.Body, &out.Balance,
		&out.Uniformity, &out.CleanCup, &out.Sweetness, &out.Overall,
		&out.DefectCat1, &out.DefectCat2, &out.TotalScore, &out.Notes, &out.Status, &cuppingAttrs,
	)
	if err != nil {
		return CuppingSession{}, fmt.Errorf("create cupping: %w", err)
	}
	out.Attrs = attrsMap(cuppingAttrs)
	_ = json.Unmarshal(scorersJSON, &out.Scorers)
	out.Grade = grade

	defects := in.DefectCat2
	if _, err := s.UpsertBatchLabSummary(ctx, UpsertLabSummaryInput{
		BatchBusinessID: sample.BatchBusinessID,
		CupScore:        &total,
		Grade:           grade,
		Defects:         &defects,
		Tester:          strings.Join(scorers, ", "),
		LatestSampleID:  sample.BusinessID,
	}); err != nil {
		return CuppingSession{}, err
	}
	if _, err := s.UpdateSampleStatus(ctx, sample.BusinessID, "complete"); err != nil {
		return CuppingSession{}, err
	}
	return out, nil
}

func (s *Store) GetLatestCuppingBySample(ctx context.Context, sampleID string) (CuppingSession, error) {
	var out CuppingSession
	var scorersJSON []byte
	var cuppingAttrs []byte
	err := s.pool.QueryRow(ctx, `
		SELECT business_id, sample_business_id, batch_business_id, session_date::text, scorers,
		       fragrance, flavor, aftertaste, acidity, body, balance, uniformity, cleancup, sweetness, overall,
		       defect_cat1, defect_cat2, total_score, notes, status, attrs
		FROM qc_cupping_sessions
		WHERE sample_business_id = $1
		ORDER BY created_at DESC LIMIT 1`, sampleID,
	).Scan(
		&out.BusinessID, &out.SampleBusinessID, &out.BatchBusinessID, &out.SessionDate, &scorersJSON,
		&out.Fragrance, &out.Flavor, &out.Aftertaste, &out.Acidity, &out.Body, &out.Balance,
		&out.Uniformity, &out.CleanCup, &out.Sweetness, &out.Overall,
		&out.DefectCat1, &out.DefectCat2, &out.TotalScore, &out.Notes, &out.Status, &cuppingAttrs,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return CuppingSession{}, ErrNotFound
		}
		return CuppingSession{}, err
	}
	_ = json.Unmarshal(scorersJSON, &out.Scorers)
	out.Attrs = attrsMap(cuppingAttrs)
	out.Grade = domain.SCATier(out.TotalScore)
	return out, nil
}

func (s *Store) ListCuppingSessions(ctx context.Context, batchID, sampleID string, limit int) ([]CuppingSession, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := `
		SELECT business_id, sample_business_id, batch_business_id, session_date::text, scorers,
		       fragrance, flavor, aftertaste, acidity, body, balance, uniformity, cleancup, sweetness, overall,
		       defect_cat1, defect_cat2, total_score, notes, status, attrs
		FROM qc_cupping_sessions WHERE 1=1`
	args := []any{}
	n := 1
	if batchID != "" {
		q += fmt.Sprintf(" AND batch_business_id = $%d", n)
		args = append(args, batchID)
		n++
	}
	if sampleID != "" {
		q += fmt.Sprintf(" AND sample_business_id = $%d", n)
		args = append(args, sampleID)
		n++
	}
	q += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d", n)
	args = append(args, limit)
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanCuppingRows(rows)
}

func (s *Store) ListCuppingBySample(ctx context.Context, sampleID string, limit int) ([]CuppingSession, error) {
	return s.ListCuppingSessions(ctx, "", sampleID, limit)
}

func (s *Store) ListCuppingByBatch(ctx context.Context, batchID string, limit int) ([]CuppingSession, error) {
	return s.ListCuppingSessions(ctx, batchID, "", limit)
}

func scanCuppingRows(rows rowScanner) ([]CuppingSession, error) {
	var out []CuppingSession
	for rows.Next() {
		var item CuppingSession
		var scorersJSON []byte
		var itemAttrs []byte
		if err := rows.Scan(
			&item.BusinessID, &item.SampleBusinessID, &item.BatchBusinessID, &item.SessionDate, &scorersJSON,
			&item.Fragrance, &item.Flavor, &item.Aftertaste, &item.Acidity, &item.Body, &item.Balance,
			&item.Uniformity, &item.CleanCup, &item.Sweetness, &item.Overall,
			&item.DefectCat1, &item.DefectCat2, &item.TotalScore, &item.Notes, &item.Status, &itemAttrs,
		); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(scorersJSON, &item.Scorers)
		item.Attrs = attrsMap(itemAttrs)
		item.Grade = domain.SCATier(item.TotalScore)
		out = append(out, item)
	}
	return out, rows.Err()
}

// panelSheet is the shape a sheet is sent to jsonb_to_recordset in.
type panelSheet struct {
	CuppingScoreInput
	TotalScore float64 `json:"total_score"`
}

// normalizePanel trims and validates the sheets: every sheet names its
// evaluator, and nobody scores the same session twice.
func normalizePanel(in []CuppingScoreInput) ([]panelSheet, error) {
	out := make([]panelSheet, 0, len(in))
	seen := map[string]bool{}
	for _, sc := range in {
		sc.Evaluator = strings.TrimSpace(sc.Evaluator)
		if sc.Evaluator == "" {
			return nil, fmt.Errorf("%w: every cupping sheet needs an evaluator", ErrBadInput)
		}
		key := strings.ToLower(sc.Evaluator)
		if seen[key] {
			return nil, fmt.Errorf("%w: %s has two sheets in one session", ErrBadInput, sc.Evaluator)
		}
		seen[key] = true
		sc.Notes = strings.TrimSpace(sc.Notes)
		out = append(out, panelSheet{
			CuppingScoreInput: sc,
			TotalScore:        domain.CalcSCATotal(sc.scoreMap(), sc.DefectCat1, sc.DefectCat2),
		})
	}
	return out, nil
}

// withPanelMean replaces a session's scores with the mean of its sheets, and
// its scorers with the evaluators who actually scored.
func withPanelMean(in CreateCuppingInput, sheets []panelSheet) CreateCuppingInput {
	n := float64(len(sheets))
	mean := func(pick func(CuppingScoreInput) float64) float64 {
		sum := 0.0
		for _, sh := range sheets {
			sum += pick(sh.CuppingScoreInput)
		}
		return math.Round(sum/n*100) / 100
	}
	in.Fragrance = mean(func(s CuppingScoreInput) float64 { return s.Fragrance })
	in.Flavor = mean(func(s CuppingScoreInput) float64 { return s.Flavor })
	in.Aftertaste = mean(func(s CuppingScoreInput) float64 { return s.Aftertaste })
	in.Acidity = mean(func(s CuppingScoreInput) float64 { return s.Acidity })
	in.Body = mean(func(s CuppingScoreInput) float64 { return s.Body })
	in.Balance = mean(func(s CuppingScoreInput) float64 { return s.Balance })
	in.Uniformity = mean(func(s CuppingScoreInput) float64 { return s.Uniformity })
	in.CleanCup = mean(func(s CuppingScoreInput) float64 { return s.CleanCup })
	in.Sweetness = mean(func(s CuppingScoreInput) float64 { return s.Sweetness })
	in.Overall = mean(func(s CuppingScoreInput) float64 { return s.Overall })
	in.DefectCat1 = int(math.Round(mean(func(s CuppingScoreInput) float64 { return float64(s.DefectCat1) })))
	in.DefectCat2 = int(math.Round(mean(func(s CuppingScoreInput) float64 { return float64(s.DefectCat2) })))
	in.Scorers = make([]string, 0, len(sheets))
	for _, sh := range sheets {
		in.Scorers = append(in.Scorers, sh.Evaluator)
	}
	return in
}

func panelMeanTotal(sheets []panelSheet) float64 {
	sum := 0.0
	for _, sh := range sheets {
		sum += sh.TotalScore
	}
	return math.Round(sum/float64(len(sheets))*100) / 100
}

// CuppingPanel is a session's sheets and how far its evaluators agreed.
type CuppingPanel struct {
	SessionBusinessID string            `json:"session_business_id"`
	Scores            []CuppingScore    `json:"scores"`
	Stats             domain.PanelStats `json:"stats"`
}

// GetCuppingPanel reads a session's sheets and computes the panel statistics.
// A session recorded without sheets returns an empty panel, not an error.
func (s *Store) GetCuppingPanel(ctx context.Context, sessionID string, threshold float64) (CuppingPanel, error) {
	sessionID = strings.TrimSpace(sessionID)
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM qc_cupping_sessions WHERE business_id = $1)`,
		sessionID).Scan(&exists); err != nil {
		return CuppingPanel{}, err
	}
	if !exists {
		return CuppingPanel{}, ErrNotFound
	}
	rows, err := s.pool.Query(ctx, `
		SELECT session_business_id, evaluator, fragrance, flavor, aftertaste, acidity, body,
		       balance, uniformity, cleancup, sweetness, overall, defect_cat1, defect_cat2,
		       total_score, notes, created_at
		FROM qc_cupping_scores WHERE session_business_id = $1
		ORDER BY evaluator`, sessionID)
	if err != nil {
		return CuppingPanel{}, err
	}
	defer rows.Close()
	panel := CuppingPanel{SessionBusinessID: sessionID, Scores: []CuppingScore{}}
	var forStats []domain.PanelScore
	for rows.Next() {
		var sc CuppingScore
		if err := rows.Scan(&sc.SessionBusinessID, &sc.Evaluator, &sc.Fragrance, &sc.Flavor,
			&sc.Aftertaste, &sc.Acidity, &sc.Body, &sc.Balance, &sc.Uniformity, &sc.CleanCup,
			&sc.Sweetness, &sc.Overall, &sc.DefectCat1, &sc.DefectCat2, &sc.TotalScore,
			&sc.Notes, &sc.CreatedAt); err != nil {
			return CuppingPanel{}, err
		}
		panel.Scores = append(panel.Scores, sc)
		forStats = append(forStats, domain.PanelScore{
			Evaluator: sc.Evaluator, Scores: sc.scoreMap(),
			DefectCat1: sc.DefectCat1, DefectCat2: sc.DefectCat2,
		})
	}
	if err := rows.Err(); err != nil {
		return CuppingPanel{}, err
	}
	panel.Stats = domain.ComputePanelStats(forStats, threshold)
	return panel, nil
}

/*
SaveCuppingScore records one evaluator's sheet against an existing session and
recomputes the session's panel mean.

This is how a panel actually works — each cupper fills in their own sheet — and
it is what a one-row-per-record screen can collect, where the scores[] on
CreateCupping needs every sheet at once. Saving the same evaluator again
corrects their sheet (the 017 change log keeps what it said before).

The first sheet on a session recorded the old way replaces its typed-in scores:
from then on the session is its panel's mean.

Two statements, not one: the recompute has to read the sheet just written, and
a data-modifying CTE's rows are invisible to the rest of its own statement. The
recompute is idempotent, so a failure between the two is corrected by the next
save on that session.
*/
func (s *Store) SaveCuppingScore(ctx context.Context, sessionID string, in CuppingScoreInput) (CuppingScore, error) {
	sessionID = strings.TrimSpace(sessionID)
	sheets, err := normalizePanel([]CuppingScoreInput{in})
	if err != nil {
		return CuppingScore{}, err
	}
	sh := sheets[0]
	var out CuppingScore
	err = s.pool.QueryRow(ctx, `
		INSERT INTO qc_cupping_scores (
			session_business_id, evaluator, fragrance, flavor, aftertaste, acidity, body,
			balance, uniformity, cleancup, sweetness, overall, defect_cat1, defect_cat2,
			total_score, notes
		)
		SELECT business_id, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16
		FROM qc_cupping_sessions WHERE business_id = $1
		ON CONFLICT (session_business_id, evaluator) DO UPDATE SET
			fragrance = EXCLUDED.fragrance, flavor = EXCLUDED.flavor,
			aftertaste = EXCLUDED.aftertaste, acidity = EXCLUDED.acidity, body = EXCLUDED.body,
			balance = EXCLUDED.balance, uniformity = EXCLUDED.uniformity,
			cleancup = EXCLUDED.cleancup, sweetness = EXCLUDED.sweetness,
			overall = EXCLUDED.overall, defect_cat1 = EXCLUDED.defect_cat1,
			defect_cat2 = EXCLUDED.defect_cat2, total_score = EXCLUDED.total_score,
			notes = EXCLUDED.notes
		RETURNING session_business_id, evaluator, fragrance, flavor, aftertaste, acidity, body,
		          balance, uniformity, cleancup, sweetness, overall, defect_cat1, defect_cat2,
		          total_score, notes, created_at`,
		sessionID, sh.Evaluator, sh.Fragrance, sh.Flavor, sh.Aftertaste, sh.Acidity, sh.Body,
		sh.Balance, sh.Uniformity, sh.CleanCup, sh.Sweetness, sh.Overall, sh.DefectCat1,
		sh.DefectCat2, sh.TotalScore, sh.Notes,
	).Scan(&out.SessionBusinessID, &out.Evaluator, &out.Fragrance, &out.Flavor, &out.Aftertaste,
		&out.Acidity, &out.Body, &out.Balance, &out.Uniformity, &out.CleanCup, &out.Sweetness,
		&out.Overall, &out.DefectCat1, &out.DefectCat2, &out.TotalScore, &out.Notes, &out.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return CuppingScore{}, ErrNotFound
	}
	if err != nil {
		return CuppingScore{}, fmt.Errorf("save cupping score: %w", err)
	}
	if err := s.recomputeCuppingSession(ctx, sessionID); err != nil {
		return out, err
	}
	return out, nil
}

// recomputeCuppingSession sets a session's scores to the mean of its sheets and
// feeds the new cup score into the batch rollup, as CreateCupping does.
func (s *Store) recomputeCuppingSession(ctx context.Context, sessionID string) error {
	var batchID, sampleID string
	var total float64
	var defects int
	var scorers []byte
	err := s.pool.QueryRow(ctx, `
		UPDATE qc_cupping_sessions s SET
			fragrance = a.fragrance, flavor = a.flavor, aftertaste = a.aftertaste,
			acidity = a.acidity, body = a.body, balance = a.balance, uniformity = a.uniformity,
			cleancup = a.cleancup, sweetness = a.sweetness, overall = a.overall,
			defect_cat1 = a.defect_cat1, defect_cat2 = a.defect_cat2,
			total_score = a.total_score, scorers = a.scorers
		FROM (
			SELECT round(avg(fragrance)::numeric, 2)::float8 AS fragrance,
			       round(avg(flavor)::numeric, 2)::float8 AS flavor,
			       round(avg(aftertaste)::numeric, 2)::float8 AS aftertaste,
			       round(avg(acidity)::numeric, 2)::float8 AS acidity,
			       round(avg(body)::numeric, 2)::float8 AS body,
			       round(avg(balance)::numeric, 2)::float8 AS balance,
			       round(avg(uniformity)::numeric, 2)::float8 AS uniformity,
			       round(avg(cleancup)::numeric, 2)::float8 AS cleancup,
			       round(avg(sweetness)::numeric, 2)::float8 AS sweetness,
			       round(avg(overall)::numeric, 2)::float8 AS overall,
			       round(avg(defect_cat1))::int AS defect_cat1,
			       round(avg(defect_cat2))::int AS defect_cat2,
			       round(avg(total_score)::numeric, 2)::float8 AS total_score,
			       jsonb_agg(evaluator ORDER BY evaluator) AS scorers
			FROM qc_cupping_scores WHERE session_business_id = $1
			HAVING COUNT(*) > 0
		) a
		WHERE s.business_id = $1
		RETURNING s.batch_business_id, s.sample_business_id, s.total_score, s.defect_cat2, s.scorers`,
		sessionID).Scan(&batchID, &sampleID, &total, &defects, &scorers)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("recompute cupping session: %w", err)
	}
	var names []string
	_ = json.Unmarshal(scorers, &names)
	_, err = s.UpsertBatchLabSummary(ctx, UpsertLabSummaryInput{
		BatchBusinessID: batchID,
		CupScore:        &total,
		Defects:         &defects,
		Tester:          strings.Join(names, ", "),
		LatestSampleID:  sampleID,
	})
	return err
}

// CuppingScoreRow is a sheet as a register lists it: with its own panel's
// verdict on it, so a screen can show who is out of line without a second call.
type CuppingScoreRow struct {
	CuppingScore
	Deviation float64 `json:"deviation"`
	Outlier   bool    `json:"outlier"`
	PanelSize int     `json:"panel_size"`
}

// ListCuppingScores lists sheets, newest sessions first, each annotated with
// its deviation within its own session's panel.
func (s *Store) ListCuppingScores(ctx context.Context, sessionID string, limit int) ([]CuppingScoreRow, error) {
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	rows, err := s.pool.Query(ctx, `
		SELECT session_business_id, evaluator, fragrance, flavor, aftertaste, acidity, body,
		       balance, uniformity, cleancup, sweetness, overall, defect_cat1, defect_cat2,
		       total_score, notes, created_at
		FROM qc_cupping_scores
		WHERE ($1 = '' OR session_business_id = $1)
		ORDER BY session_business_id DESC, evaluator
		LIMIT $2`, strings.TrimSpace(sessionID), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var all []CuppingScore
	for rows.Next() {
		var sc CuppingScore
		if err := rows.Scan(&sc.SessionBusinessID, &sc.Evaluator, &sc.Fragrance, &sc.Flavor,
			&sc.Aftertaste, &sc.Acidity, &sc.Body, &sc.Balance, &sc.Uniformity, &sc.CleanCup,
			&sc.Sweetness, &sc.Overall, &sc.DefectCat1, &sc.DefectCat2, &sc.TotalScore,
			&sc.Notes, &sc.CreatedAt); err != nil {
			return nil, err
		}
		all = append(all, sc)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	bySession := map[string][]domain.PanelScore{}
	for _, sc := range all {
		bySession[sc.SessionBusinessID] = append(bySession[sc.SessionBusinessID], domain.PanelScore{
			Evaluator: sc.Evaluator, Scores: sc.scoreMap(), DefectCat1: sc.DefectCat1, DefectCat2: sc.DefectCat2,
		})
	}
	devs := map[string]domain.EvaluatorDeviation{}
	sizes := map[string]int{}
	for session, panel := range bySession {
		stats := domain.ComputePanelStats(panel, 0)
		sizes[session] = stats.PanelSize
		for _, ev := range stats.Evaluators {
			devs[session+"\x00"+ev.Evaluator] = ev
		}
	}
	out := make([]CuppingScoreRow, 0, len(all))
	for _, sc := range all {
		d := devs[sc.SessionBusinessID+"\x00"+sc.Evaluator]
		out = append(out, CuppingScoreRow{CuppingScore: sc, Deviation: d.Deviation, Outlier: d.Outlier,
			PanelSize: sizes[sc.SessionBusinessID]})
	}
	return out, nil
}
