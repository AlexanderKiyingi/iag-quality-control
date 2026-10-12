package store

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"iag-quality-control/backend/internal/domain"
)

type SPCPoint struct {
	Timestamp string  `json:"timestamp"`
	Value     float64 `json:"value"`
	Subgroup  string  `json:"subgroup,omitempty"`
	// Ref is the record the reading came from; Batch is its batch.
	Ref       string `json:"ref,omitempty"`
	Batch     string `json:"batch,omitempty"`
	OutOfCtrl bool   `json:"out_of_control"`
	OutOfSpec bool   `json:"out_of_spec"`
	// Rules lists the Nelson rules this point completes (domain.Rules).
	Rules []int `json:"rules,omitempty"`
}

// SPCSubgroup is one plotted mean on an X̄ chart.
type SPCSubgroup struct {
	domain.Subgroup
	FirstSeen string `json:"first_seen"`
	OutOfCtrl bool   `json:"out_of_control"`
	Rules     []int  `json:"rules,omitempty"`
}

// SPCProportion is one plotted fraction on a p chart.
type SPCProportion struct {
	domain.Proportion
	OutOfCtrl bool  `json:"out_of_control"`
	Rules     []int `json:"rules,omitempty"`
}

type SPCSeries struct {
	Metric          string  `json:"metric"`
	Chart           string  `json:"chart"`
	BatchBusinessID string  `json:"batch_business_id,omitempty"`
	Days            int     `json:"days"`
	Count           int     `json:"count"`
	Mean            float64 `json:"mean"`
	StdDev          float64 `json:"std_dev"`
	UCL             float64 `json:"ucl"`
	LCL             float64 `json:"lcl"`
	// Sigma is the short-term σ the limits were set from.
	Sigma             float64    `json:"sigma"`
	USL               *float64   `json:"usl,omitempty"`
	LSL               *float64   `json:"lsl,omitempty"`
	Cp                *float64   `json:"cp,omitempty"`
	Cpk               *float64   `json:"cpk,omitempty"`
	Points            []SPCPoint `json:"points"`
	OutOfControlCount int        `json:"out_of_control_count"`
	OutOfSpecCount    int        `json:"out_of_spec_count"`
	// SpecID names the specification the limits came from, when they did.
	SpecID string `json:"spec_id,omitempty"`
	// I-MR: the moving ranges (one fewer than points) and their limits.
	MovingRanges []float64      `json:"moving_ranges,omitempty"`
	MRLimits     *domain.Limits `json:"mr_limits,omitempty"`
	// X̄-R / X̄-S: the subgroups, how they were formed, and how many single
	// readings could not form one.
	SubgroupBy string        `json:"subgroup_by,omitempty"`
	Subgroups  []SPCSubgroup `json:"subgroups,omitempty"`
	Dropped    int           `json:"dropped,omitempty"`
	// p chart.
	Proportions []SPCProportion `json:"proportions,omitempty"`
	// Rule breaks across the chart, and what each rule means.
	RuleBreaks int            `json:"rule_breaks"`
	RuleNames  map[int]string `json:"rule_names"`
}

// Chart types SPC draws.
const (
	ChartIMR   = "i_mr"
	ChartXBarR = "xbar_r"
	ChartXBarS = "xbar_s"
	ChartP     = "p"
	ChartC     = "c"
)

type SPCOptions struct {
	Metric  string
	BatchID string
	Days    int
	USL     *float64
	LSL     *float64
	SpecID  string
	// Chart is one of the Chart* constants; i_mr when empty.
	Chart string
	// SubgroupBy is batch (default), sample or day, for X̄ and p charts.
	SubgroupBy string
	// Rules to test; domain.DefaultRules when nil.
	Rules []int
}

/*
SPC builds a control chart for one parameter.

Any parameter recorded through qc_lab_measurements can be charted (013 made
any analyte recordable; until now only moisture and cup score could be
plotted). The two metrics that also have fixed-column homes read both: moisture
from physical tests AND measurements, cup score from cupping sessions AND
measurements — otherwise a lab that moved to the Results screen would watch its
moisture chart go quiet. Voided measurements are left out.

Measurement parameters are free text, so they are matched by
CanonicalParameter in Go rather than in SQL; the window is capped at 90 days
and the scan at spcScanLimit rows.
*/
func (s *Store) SPC(ctx context.Context, opts SPCOptions) (SPCSeries, error) {
	batchID := strings.TrimSpace(opts.BatchID)
	days := opts.Days
	metric := CanonicalParameter(opts.Metric)
	if strings.TrimSpace(opts.Metric) == "" {
		metric = "moisture"
	}
	if metric == "" {
		return SPCSeries{}, ErrBadInput
	}
	if days <= 0 || days > 90 {
		days = 30
	}
	chart := strings.TrimSpace(opts.Chart)
	if chart == "" {
		chart = ChartIMR
	}
	switch chart {
	case ChartIMR, ChartXBarR, ChartXBarS, ChartP, ChartC:
	default:
		return SPCSeries{}, fmt.Errorf("%w: chart is i_mr, xbar_r, xbar_s, p or c", ErrBadInput)
	}
	rules := opts.Rules
	if rules == nil {
		rules = domain.DefaultRules
	}

	var points []SPCPoint
	if metric == "failure_rate" {
		var err error
		if points, err = s.verdictPoints(ctx, days, batchID); err != nil {
			return SPCSeries{}, err
		}
		chart = ChartP
	} else {
		var err error
		if points, err = s.spcPoints(ctx, metric, days, batchID); err != nil {
			return SPCSeries{}, err
		}
	}

	series := SPCSeries{
		Metric: metric, Chart: chart, BatchBusinessID: batchID, Days: days, Count: len(points),
		Points: points, USL: opts.USL, LSL: opts.LSL, SpecID: opts.SpecID, RuleNames: domain.Rules,
	}
	if series.Points == nil {
		series.Points = []SPCPoint{}
	}
	if len(points) == 0 {
		return series, nil
	}
	values := make([]float64, len(points))
	for i, p := range points {
		values[i] = p.Value
		if metric != "failure_rate" && !withinSpec(p.Value, opts.LSL, opts.USL) {
			series.Points[i].OutOfSpec = true
			series.OutOfSpecCount++
		}
	}
	mean, std := meanStd(values)
	series.Mean, series.StdDev = round2(mean), round2(std)

	switch chart {
	case ChartIMR, ChartC:
		var band domain.Limits
		if chart == ChartC {
			band = domain.CChart(values)
		} else {
			var mrLimits domain.Limits
			band, series.MovingRanges, mrLimits = domain.IMR(values)
			series.MRLimits = &mrLimits
		}
		series.UCL, series.LCL, series.Sigma = band.UCL, band.LCL, band.Sigma
		broken := domain.ApplyRules(values, []domain.Limits{band}, rules)
		for i := range series.Points {
			series.Points[i].Rules = broken[i]
			if values[i] > band.UCL || values[i] < band.LCL {
				series.Points[i].OutOfCtrl = true
				series.OutOfControlCount++
			}
			if len(broken[i]) > 0 {
				series.RuleBreaks++
			}
		}
	case ChartXBarR, ChartXBarS:
		series.SubgroupBy = subgroupBy(opts.SubgroupBy)
		groups, firstSeen := groupPoints(points, series.SubgroupBy)
		out, grand, dropped := domain.XBar(groups, chart == ChartXBarS)
		series.Dropped = dropped
		series.Mean = round2(grand)
		means := make([]float64, len(out))
		limits := make([]domain.Limits, len(out))
		for i, g := range out {
			means[i], limits[i] = g.Mean, g.Limits
		}
		broken := domain.ApplyRules(means, limits, rules)
		for i, g := range out {
			sg := SPCSubgroup{Subgroup: g, FirstSeen: firstSeen[g.Key], Rules: broken[i]}
			sg.OutOfCtrl = g.Mean > g.Limits.UCL || g.Mean < g.Limits.LCL
			if sg.OutOfCtrl {
				series.OutOfControlCount++
			}
			if len(broken[i]) > 0 {
				series.RuleBreaks++
			}
			series.Subgroups = append(series.Subgroups, sg)
		}
		if len(out) > 0 {
			series.UCL, series.LCL, series.Sigma = out[0].Limits.UCL, out[0].Limits.LCL, out[0].Limits.Sigma
		}
	case ChartP:
		if metric != "failure_rate" && opts.USL == nil && opts.LSL == nil {
			return SPCSeries{}, fmt.Errorf("%w: a p chart of %s needs spec limits to count failures against", ErrBadInput, metric)
		}
		by := subgroupBy(opts.SubgroupBy)
		if opts.SubgroupBy == "" {
			by = "day"
		}
		series.SubgroupBy = by
		order := []string{}
		props := map[string]*domain.Proportion{}
		for _, p := range series.Points {
			k := subgroupKey(p, by)
			if props[k] == nil {
				props[k] = &domain.Proportion{Key: k}
				order = append(order, k)
			}
			props[k].Inspected++
			failed := p.OutOfSpec
			if metric == "failure_rate" {
				failed = p.Value > 0
			}
			if failed {
				props[k].Defective++
			}
		}
		in := make([]domain.Proportion, 0, len(order))
		for _, k := range order {
			in = append(in, *props[k])
		}
		out := domain.PChart(in)
		ps := make([]float64, len(out))
		limits := make([]domain.Limits, len(out))
		for i, g := range out {
			ps[i], limits[i] = g.P, g.Limits
		}
		broken := domain.ApplyRules(ps, limits, rules)
		for i, g := range out {
			sp := SPCProportion{Proportion: g, Rules: broken[i], OutOfCtrl: g.P > g.Limits.UCL || g.P < g.Limits.LCL}
			if sp.OutOfCtrl {
				series.OutOfControlCount++
			}
			if len(broken[i]) > 0 {
				series.RuleBreaks++
			}
			series.Proportions = append(series.Proportions, sp)
		}
		if len(out) > 0 {
			series.Mean = out[0].Limits.Center
		}
	}
	if chart != ChartP {
		series.Cp, series.Cpk = capability(mean, std, opts.LSL, opts.USL)
	}
	return series, nil
}

func subgroupBy(v string) string {
	switch strings.TrimSpace(v) {
	case "sample", "day":
		return v
	default:
		return "batch"
	}
}

func subgroupKey(p SPCPoint, by string) string {
	switch by {
	case "sample":
		return p.Subgroup
	case "day":
		if len(p.Timestamp) >= 10 {
			return p.Timestamp[:10]
		}
		return p.Timestamp
	default:
		if p.Batch == "" {
			return p.Subgroup
		}
		return p.Batch
	}
}

// groupPoints forms subgroups in the order they were first seen.
func groupPoints(points []SPCPoint, by string) ([]domain.Subgroup, map[string]string) {
	index := map[string]int{}
	firstSeen := map[string]string{}
	var groups []domain.Subgroup
	for _, p := range points {
		k := subgroupKey(p, by)
		i, ok := index[k]
		if !ok {
			i = len(groups)
			index[k] = i
			firstSeen[k] = p.Timestamp
			groups = append(groups, domain.Subgroup{Key: k})
		}
		groups[i].Values = append(groups[i].Values, p.Value)
	}
	return groups, firstSeen
}

/*
spcPoints reads one parameter's readings, oldest first, from every table that
records it: physical tests (moisture), cupping sessions (cup score), green
gradings (021 metrics) and measurements. Measurement parameters are free text,
so they are matched by CanonicalParameter in Go; the scan is capped.
*/
func (s *Store) spcPoints(ctx context.Context, metric string, days int, batchID string) ([]SPCPoint, error) {
	var points []SPCPoint
	appendRows := func(q string, args ...any) error {
		rows, err := s.pool.Query(ctx, q, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var p SPCPoint
			if err := rows.Scan(&p.Timestamp, &p.Value, &p.Subgroup, &p.Ref, &p.Batch); err != nil {
				return err
			}
			points = append(points, p)
		}
		return rows.Err()
	}
	switch metric {
	case "moisture":
		if err := appendRows(`
			SELECT tested_at::text, moisture_pct::float8, sample_business_id, business_id, batch_business_id
			FROM qc_physical_tests
			WHERE moisture_pct IS NOT NULL
			  AND tested_at >= NOW() - ($1::int || ' days')::interval
			  AND ($2 = '' OR batch_business_id = $2)`, days, batchID); err != nil {
			return nil, err
		}
	case "cup_score":
		if err := appendRows(`
			SELECT created_at::text, total_score::float8, sample_business_id, business_id, batch_business_id
			FROM qc_cupping_sessions
			WHERE created_at >= NOW() - ($1::int || ' days')::interval
			  AND ($2 = '' OR batch_business_id = $2)`, days, batchID); err != nil {
			return nil, err
		}
	}
	// Green gradings (021) carry their derived metrics by canonical name.
	if err := appendRows(`
		SELECT graded_at::text, (metrics->>$3)::float8, sample_business_id, business_id, batch_business_id
		FROM qc_green_gradings
		WHERE status <> 'void' AND metrics ? $3
		  AND graded_at >= NOW() - ($1::int || ' days')::interval
		  AND ($2 = '' OR batch_business_id = $2)`, days, batchID, metric); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT created_at::text, value_num::float8, sample_business_id, business_id, batch_business_id, parameter
		FROM qc_lab_measurements
		WHERE value_num IS NOT NULL AND status <> 'void'
		  AND created_at >= NOW() - ($1::int || ' days')::interval
		  AND ($2 = '' OR batch_business_id = $2)
		ORDER BY created_at DESC
		LIMIT $3`, days, batchID, spcScanLimit)
	if err != nil {
		return nil, err
	}
	err = func() error {
		defer rows.Close()
		for rows.Next() {
			var p SPCPoint
			var parameter string
			if err := rows.Scan(&p.Timestamp, &p.Value, &p.Subgroup, &p.Ref, &p.Batch, &parameter); err != nil {
				return err
			}
			if CanonicalParameter(parameter) == metric {
				points = append(points, p)
			}
		}
		return rows.Err()
	}()
	if err != nil {
		return nil, err
	}
	// Every source renders timestamps the same way (timestamptz::text in one
	// session), so a string sort is a time sort.
	sort.SliceStable(points, func(i, j int) bool { return points[i].Timestamp < points[j].Timestamp })
	return points, nil
}

/*
verdictPoints reads every judged record — measurements, incoming inspections,
in-process checks, green gradings — as a 1 (fail) or 0 (pass), for the
failure-rate p chart. Records nobody judged (no spec, not numeric) are not
inspections and are left out.
*/
func (s *Store) verdictPoints(ctx context.Context, days int, batchID string) ([]SPCPoint, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT ts, CASE WHEN verdict = 'fail' THEN 1 ELSE 0 END::float8, subgroup, ref, batch FROM (
			SELECT created_at::text ts, verdict, sample_business_id subgroup, business_id ref, batch_business_id batch
			FROM qc_lab_measurements WHERE status <> 'void' AND created_at >= NOW() - ($1::int || ' days')::interval
			UNION ALL
			SELECT created_at::text, verdict, source_lot, business_id, source_lot
			FROM qc_incoming_inspections WHERE created_at >= NOW() - ($1::int || ' days')::interval
			UNION ALL
			SELECT created_at::text, verdict, batch_ref, business_id, batch_ref
			FROM qc_in_process_checks WHERE created_at >= NOW() - ($1::int || ' days')::interval
			UNION ALL
			SELECT graded_at::text, verdict, sample_business_id, business_id, batch_business_id
			FROM qc_green_gradings WHERE status <> 'void' AND graded_at >= NOW() - ($1::int || ' days')::interval
		) v
		WHERE verdict IN ('pass', 'fail') AND ($2 = '' OR batch = $2)
		ORDER BY ts`, days, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var points []SPCPoint
	for rows.Next() {
		var p SPCPoint
		if err := rows.Scan(&p.Timestamp, &p.Value, &p.Subgroup, &p.Ref, &p.Batch); err != nil {
			return nil, err
		}
		points = append(points, p)
	}
	return points, rows.Err()
}

// spcScanLimit caps how many measurement rows one chart reads.
const spcScanLimit = 5000

func withinSpec(v float64, lsl, usl *float64) bool {
	return (lsl == nil || v >= *lsl) && (usl == nil || v <= *usl)
}

/*
capability returns Cp and Cpk.

Cp needs both limits — it is the spread of the band over the spread of the
process. Cpk does not: with one limit it is the one-sided index (Cpu or Cpl),
which is the honest figure for a spec like moisture <= 12.5% that has no floor.
Before 016 the moisture chart borrowed an invented LSL of 10.3 to get a Cpk at
all. With no spread there is nothing to divide by, so neither is reported.
*/
func capability(mean, std float64, lsl, usl *float64) (cp, cpk *float64) {
	if std <= 0 || (lsl == nil && usl == nil) {
		return nil, nil
	}
	var k float64
	switch {
	case lsl != nil && usl != nil:
		c := round2((*usl - *lsl) / (6 * std))
		cp = &c
		k = math.Min((*usl-mean)/(3*std), (mean-*lsl)/(3*std))
	case usl != nil:
		k = (*usl - mean) / (3 * std)
	default:
		k = (mean - *lsl) / (3 * std)
	}
	k = round2(k)
	return cp, &k
}

// SPCParameter is one chartable parameter and how much data it has.
type SPCParameter struct {
	Parameter string `json:"parameter"`
	Count     int    `json:"count"`
}

// SPCParameters lists what can be charted over a window, busiest first, so a
// chart screen can offer a picker instead of a free-text box.
func (s *Store) SPCParameters(ctx context.Context, days int) ([]SPCParameter, error) {
	if days <= 0 || days > 90 {
		days = 30
	}
	counts := map[string]int{}
	add := func(q string) error {
		rows, err := s.pool.Query(ctx, q, days)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var p string
			var n int
			if err := rows.Scan(&p, &n); err != nil {
				return err
			}
			if c := CanonicalParameter(p); c != "" {
				counts[c] += n
			}
		}
		return rows.Err()
	}
	for _, q := range []string{
		`SELECT 'moisture', COUNT(*) FROM qc_physical_tests
		 WHERE moisture_pct IS NOT NULL AND tested_at >= NOW() - ($1::int || ' days')::interval`,
		`SELECT 'cup_score', COUNT(*) FROM qc_cupping_sessions
		 WHERE created_at >= NOW() - ($1::int || ' days')::interval`,
		`SELECT m.key, COUNT(*) FROM qc_green_gradings g, jsonb_each(g.metrics) m
		 WHERE g.status <> 'void' AND g.graded_at >= NOW() - ($1::int || ' days')::interval
		 GROUP BY m.key`,
		`SELECT parameter, COUNT(*) FROM qc_lab_measurements
		 WHERE value_num IS NOT NULL AND status <> 'void'
		   AND created_at >= NOW() - ($1::int || ' days')::interval
		 GROUP BY parameter`,
	} {
		if err := add(q); err != nil {
			return nil, err
		}
	}
	out := make([]SPCParameter, 0, len(counts))
	for p, n := range counts {
		if n > 0 {
			out = append(out, SPCParameter{Parameter: p, Count: n})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Parameter < out[j].Parameter
	})
	return out, nil
}

func meanStd(vals []float64) (float64, float64) {
	if len(vals) == 0 {
		return 0, 0
	}
	var sum float64
	for _, v := range vals {
		sum += v
	}
	mean := sum / float64(len(vals))
	if len(vals) < 2 {
		return mean, 0
	}
	var sq float64
	for _, v := range vals {
		d := v - mean
		sq += d * d
	}
	return mean, math.Sqrt(sq / float64(len(vals)-1))
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}
