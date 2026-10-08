package store

import (
	"context"
	"math"
	"sort"
	"strings"
)

type SPCPoint struct {
	Timestamp string  `json:"timestamp"`
	Value     float64 `json:"value"`
	Subgroup  string  `json:"subgroup,omitempty"`
	OutOfCtrl bool    `json:"out_of_control"`
	OutOfSpec bool    `json:"out_of_spec"`
}

type SPCSeries struct {
	Metric            string     `json:"metric"`
	BatchBusinessID   string     `json:"batch_business_id,omitempty"`
	Days              int        `json:"days"`
	Count             int        `json:"count"`
	Mean              float64    `json:"mean"`
	StdDev            float64    `json:"std_dev"`
	UCL               float64    `json:"ucl"`
	LCL               float64    `json:"lcl"`
	USL               *float64   `json:"usl,omitempty"`
	LSL               *float64   `json:"lsl,omitempty"`
	Cp                *float64   `json:"cp,omitempty"`
	Cpk               *float64   `json:"cpk,omitempty"`
	Points            []SPCPoint `json:"points"`
	OutOfControlCount int        `json:"out_of_control_count"`
	OutOfSpecCount    int        `json:"out_of_spec_count"`
	// SpecID names the specification the limits came from, when they did.
	SpecID string `json:"spec_id,omitempty"`
}

type SPCOptions struct {
	Metric  string
	BatchID string
	Days    int
	USL     *float64
	LSL     *float64
	SpecID  string
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

	var points []SPCPoint
	appendRows := func(q string, args ...any) error {
		rows, err := s.pool.Query(ctx, q, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var ts, subgroup string
			var val float64
			if err := rows.Scan(&ts, &val, &subgroup); err != nil {
				return err
			}
			points = append(points, SPCPoint{Timestamp: ts, Value: val, Subgroup: subgroup})
		}
		return rows.Err()
	}

	switch metric {
	case "moisture":
		if err := appendRows(`
			SELECT tested_at::text, moisture_pct::float8, sample_business_id
			FROM qc_physical_tests
			WHERE moisture_pct IS NOT NULL
			  AND tested_at >= NOW() - ($1::int || ' days')::interval
			  AND ($2 = '' OR batch_business_id = $2)`, days, batchID); err != nil {
			return SPCSeries{}, err
		}
	case "cup_score":
		if err := appendRows(`
			SELECT created_at::text, total_score::float8, sample_business_id
			FROM qc_cupping_sessions
			WHERE created_at >= NOW() - ($1::int || ' days')::interval
			  AND ($2 = '' OR batch_business_id = $2)`, days, batchID); err != nil {
			return SPCSeries{}, err
		}
	}

	rows, err := s.pool.Query(ctx, `
		SELECT created_at::text, value_num::float8, sample_business_id, parameter
		FROM qc_lab_measurements
		WHERE value_num IS NOT NULL AND status <> 'void'
		  AND created_at >= NOW() - ($1::int || ' days')::interval
		  AND ($2 = '' OR batch_business_id = $2)
		ORDER BY created_at DESC
		LIMIT $3`, days, batchID, spcScanLimit)
	if err != nil {
		return SPCSeries{}, err
	}
	err = func() error {
		defer rows.Close()
		for rows.Next() {
			var ts, subgroup, parameter string
			var val float64
			if err := rows.Scan(&ts, &val, &subgroup, &parameter); err != nil {
				return err
			}
			if CanonicalParameter(parameter) == metric {
				points = append(points, SPCPoint{Timestamp: ts, Value: val, Subgroup: subgroup})
			}
		}
		return rows.Err()
	}()
	if err != nil {
		return SPCSeries{}, err
	}
	// Both sources render timestamps the same way (timestamptz::text in one
	// session), so a string sort is a time sort.
	sort.SliceStable(points, func(i, j int) bool { return points[i].Timestamp < points[j].Timestamp })

	values := make([]float64, len(points))
	for i, p := range points {
		values[i] = p.Value
	}
	series := SPCSeries{
		Metric:          metric,
		BatchBusinessID: batchID,
		Days:            days,
		Count:           len(values),
		Points:          points,
		USL:             opts.USL,
		LSL:             opts.LSL,
		SpecID:          opts.SpecID,
	}
	if series.Points == nil {
		series.Points = []SPCPoint{}
	}
	if len(values) == 0 {
		return series, nil
	}
	mean, std := meanStd(values)
	series.Mean = round2(mean)
	series.StdDev = round2(std)
	series.UCL = round2(mean + 3*std)
	series.LCL = round2(mean - 3*std)
	for i := range series.Points {
		v := series.Points[i].Value
		if v > series.UCL || v < series.LCL {
			series.Points[i].OutOfCtrl = true
			series.OutOfControlCount++
		}
		if !withinSpec(v, opts.LSL, opts.USL) {
			series.Points[i].OutOfSpec = true
			series.OutOfSpecCount++
		}
	}
	series.Cp, series.Cpk = capability(mean, std, opts.LSL, opts.USL)
	return series, nil
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
