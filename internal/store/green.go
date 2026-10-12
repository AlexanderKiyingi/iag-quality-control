package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"iag-quality-control/backend/internal/domain"
)

// GreenGrading is one grading of a green coffee sample (migration 021).
type GreenGrading struct {
	BusinessID       string             `json:"business_id"`
	SampleBusinessID string             `json:"sample_business_id"`
	BatchBusinessID  string             `json:"batch_business_id"`
	SampleGrams      float64            `json:"sample_grams"`
	MoisturePct      *float64           `json:"moisture_pct,omitempty"`
	WaterActivity    *float64           `json:"water_activity,omitempty"`
	DensityGL        *float64           `json:"density_g_l,omitempty"`
	Quakers          *int               `json:"quakers,omitempty"`
	Color            string             `json:"color"`
	Odor             string             `json:"odor"`
	Defects          map[string]int     `json:"defects"`
	Screens          map[string]float64 `json:"screens"`
	Result           domain.GreenResult `json:"result"`
	FullDefects      float64            `json:"full_defects"`
	SCAGrade         string             `json:"sca_grade"`
	Grader           string             `json:"grader"`
	Stage            string             `json:"stage"`
	GradeTarget      string             `json:"grade_target"`
	Verdict          string             `json:"verdict"`
	Evaluation       []EvaluationEntry  `json:"evaluation"`
	Status           string             `json:"status"`
	Notes            string             `json:"notes"`
	UpdatedBy        string             `json:"updated_by"`
	ChangeReason     string             `json:"change_reason"`
	GradedAt         time.Time          `json:"graded_at"`
	// What a failing grading set in motion; only on the write response.
	AutoActions *AutoActions `json:"auto_actions,omitempty"`
}

// CreateGreenGradingInput is a grader's sheet.
type CreateGreenGradingInput struct {
	SampleBusinessID string             `json:"sample_business_id"`
	SampleGrams      float64            `json:"sample_grams"`
	MoisturePct      *float64           `json:"moisture_pct"`
	WaterActivity    *float64           `json:"water_activity"`
	DensityGL        *float64           `json:"density_g_l"`
	Quakers          *int               `json:"quakers"`
	Color            string             `json:"color"`
	Odor             string             `json:"odor"`
	Defects          map[string]int     `json:"defects"`
	Screens          map[string]float64 `json:"screens"`
	Grader           string             `json:"grader"`
	// Stage and GradeTarget pick the specs; both fall back to the sample's
	// attrs. GradeTarget is the grade the lot is meant to make ("Screen 18").
	Stage       string `json:"stage"`
	GradeTarget string `json:"grade_target"`
	Notes       string `json:"notes"`
	Actor       string `json:"-"`
}

const greenCols = `business_id, sample_business_id, batch_business_id, sample_grams, moisture_pct,
	water_activity, density_g_l, quakers, color, odor, defects, screens, result, full_defects,
	sca_grade, grader, stage, grade_target, verdict, evaluation, status, notes, updated_by,
	change_reason, graded_at`

func scanGreen(row interface{ Scan(...any) error }) (GreenGrading, error) {
	var g GreenGrading
	var defects, screens, result, evaluation []byte
	if err := row.Scan(&g.BusinessID, &g.SampleBusinessID, &g.BatchBusinessID, &g.SampleGrams,
		&g.MoisturePct, &g.WaterActivity, &g.DensityGL, &g.Quakers, &g.Color, &g.Odor,
		&defects, &screens, &result, &g.FullDefects, &g.SCAGrade, &g.Grader, &g.Stage,
		&g.GradeTarget, &g.Verdict, &evaluation, &g.Status, &g.Notes, &g.UpdatedBy,
		&g.ChangeReason, &g.GradedAt); err != nil {
		return GreenGrading{}, err
	}
	g.Defects, g.Screens, g.Evaluation = map[string]int{}, map[string]float64{}, []EvaluationEntry{}
	_ = json.Unmarshal(defects, &g.Defects)
	_ = json.Unmarshal(screens, &g.Screens)
	_ = json.Unmarshal(result, &g.Result)
	_ = json.Unmarshal(evaluation, &g.Evaluation)
	return g, nil
}

/*
CreateGreenGrading records a grading, works out its full defects, screen
distribution and SCA grade, and judges its metrics against the active specs —
so a failing lot raises its NC (and hold) exactly as a failing measurement
does. Moisture, water activity and defects feed the batch rollup.
*/
func (s *Store) CreateGreenGrading(ctx context.Context, in CreateGreenGradingInput) (GreenGrading, error) {
	sample, err := s.GetSample(ctx, strings.TrimSpace(in.SampleBusinessID))
	if err != nil {
		return GreenGrading{}, err
	}
	gin := domain.GreenInput{SampleGrams: in.SampleGrams, Defects: in.Defects, Screens: in.Screens,
		MoisturePct: in.MoisturePct, WaterActivity: in.WaterActivity, DensityGL: in.DensityGL, Quakers: in.Quakers}
	if gin.SampleGrams == 0 {
		gin.SampleGrams = domain.StandardSampleGrams
	}
	if err := domain.ValidateGreen(gin); err != nil {
		return GreenGrading{}, fmt.Errorf("%w: %s", ErrBadInput, err.Error())
	}
	res := domain.GradeGreen(gin)

	stage := strings.TrimSpace(in.Stage)
	if stage == "" {
		stage = attrString(sample.Attrs, "stage")
	}
	if stage == "" {
		stage = "incoming"
	}
	if stage = NormalizeSpecStage(stage); stage == "" {
		return GreenGrading{}, fmt.Errorf("%w: stage must be incoming, in_process, finished or any", ErrBadInput)
	}
	target := strings.TrimSpace(in.GradeTarget)
	if target == "" {
		target = attrString(sample.Attrs, "grade")
	}
	readings := make([]Reading, 0, len(res.Metrics))
	for _, k := range domain.SortedMetricKeys(res.Metrics) {
		v := res.Metrics[k]
		readings = append(readings, Reading{Parameter: k, Raw: trimFloatStr(v), Value: &v})
	}
	ev, err := s.Evaluate(ctx, stage, target, readings)
	if err != nil {
		return GreenGrading{}, err
	}

	id, err := nextBusinessID(ctx, s.pool, "qc_green_gradings", "GRN")
	if err != nil {
		return GreenGrading{}, err
	}
	defects, _ := json.Marshal(nonNilInts(in.Defects))
	screens, _ := json.Marshal(nonNilFloats(in.Screens))
	result, _ := json.Marshal(res)
	metrics, _ := json.Marshal(res.Metrics)
	actor := strings.TrimSpace(in.Actor)
	grader := strings.TrimSpace(in.Grader)
	if grader == "" {
		grader = actor
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO qc_green_gradings (business_id, sample_business_id, batch_business_id, sample_grams,
			moisture_pct, water_activity, density_g_l, quakers, color, odor, defects, screens, result,
			metrics, full_defects, sca_grade, grader, stage, grade_target, verdict, evaluation, notes,
			updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb,$12::jsonb,$13::jsonb,$14::jsonb,$15,$16,$17,
			$18,$19,$20,$21::jsonb,$22,$23)
		RETURNING `+greenCols,
		id, sample.BusinessID, sample.BatchBusinessID, gin.SampleGrams, in.MoisturePct, in.WaterActivity,
		in.DensityGL, in.Quakers, strings.TrimSpace(in.Color), strings.TrimSpace(in.Odor), defects, screens,
		result, metrics, res.TotalPer350, res.Grade, grader, stage, target, ev.Verdict, ev.EntriesJSON(),
		in.Notes, actor)
	item, err := scanGreen(row)
	if err != nil {
		return GreenGrading{}, fmt.Errorf("create green grading: %w", err)
	}

	auto, err := s.raiseAutoActions(ctx, autoActionInput{
		SourceRef: item.BusinessID, SourceKind: "green grading", HoldRef: item.BatchBusinessID,
		Actor: actor, Evaluation: ev,
	})
	if err != nil {
		return item, err
	}
	item.AutoActions = auto

	if item.BatchBusinessID != "" {
		total := res.Total
		roll := UpsertLabSummaryInput{BatchBusinessID: item.BatchBusinessID, Tester: grader,
			LatestSampleID: item.SampleBusinessID, Moisture: in.MoisturePct, WaterActivity: in.WaterActivity,
			Defects: &total}
		if _, err := s.UpsertBatchLabSummary(ctx, roll); err != nil {
			return item, err
		}
	}
	if _, err := s.UpdateSampleStatus(ctx, sample.BusinessID, "in-progress"); err != nil {
		return item, err
	}
	return item, nil
}

// ListGreenGradings lists gradings, newest first.
func (s *Store) ListGreenGradings(ctx context.Context, sampleID, batchID string, includeVoid bool, limit int) ([]GreenGrading, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.pool.Query(ctx, `SELECT `+greenCols+` FROM qc_green_gradings
		WHERE ($1 = '' OR sample_business_id = $1) AND ($2 = '' OR batch_business_id = $2)
		  AND ($3 OR status <> 'void')
		ORDER BY graded_at DESC LIMIT $4`,
		strings.TrimSpace(sampleID), strings.TrimSpace(batchID), includeVoid, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []GreenGrading{}
	for rows.Next() {
		g, err := scanGreen(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *Store) GetGreenGrading(ctx context.Context, id string) (GreenGrading, error) {
	g, err := scanGreen(s.pool.QueryRow(ctx, `SELECT `+greenCols+` FROM qc_green_gradings WHERE business_id = $1`,
		strings.TrimSpace(id)))
	if errors.Is(err, pgx.ErrNoRows) {
		return GreenGrading{}, ErrNotFound
	}
	return g, err
}

// VoidGreenGrading withdraws a grading that was wrong. It stays on record —
// the change log keeps who voided it and why — but leaves lists and charts.
func (s *Store) VoidGreenGrading(ctx context.Context, id, actor, reason string) (GreenGrading, error) {
	if strings.TrimSpace(reason) == "" {
		return GreenGrading{}, ErrReasonRequired
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE qc_green_gradings SET status = 'void', updated_by = $2, change_reason = $3, updated_at = NOW()
		WHERE business_id = $1 AND status <> 'void'`, strings.TrimSpace(id), strings.TrimSpace(actor), strings.TrimSpace(reason))
	if err != nil {
		return GreenGrading{}, err
	}
	if tag.RowsAffected() == 0 {
		if _, err := s.GetGreenGrading(ctx, id); err != nil {
			return GreenGrading{}, err
		}
		return GreenGrading{}, fmt.Errorf("%w: this grading is already void", ErrConflict)
	}
	return s.GetGreenGrading(ctx, id)
}

func trimFloatStr(v float64) string { return fmt.Sprintf("%g", v) }

func nonNilInts(m map[string]int) map[string]int {
	if m == nil {
		return map[string]int{}
	}
	return m
}

func nonNilFloats(m map[string]float64) map[string]float64 {
	if m == nil {
		return map[string]float64{}
	}
	return m
}
