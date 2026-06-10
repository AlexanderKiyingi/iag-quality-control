package store

import (
	"context"
	"math"
	"strings"
)

type SPCPoint struct {
	Timestamp string   `json:"timestamp"`
	Value     float64  `json:"value"`
	Subgroup  string   `json:"subgroup,omitempty"`
	OutOfCtrl bool     `json:"out_of_control"`
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
}

type SPCOptions struct {
	Metric   string
	BatchID  string
	Days     int
	USL      *float64
	LSL      *float64
}

func (s *Store) SPC(ctx context.Context, opts SPCOptions) (SPCSeries, error) {
	metric := opts.Metric
	batchID := opts.BatchID
	days := opts.Days
	metric = strings.ToLower(strings.TrimSpace(metric))
	if metric == "" {
		metric = "moisture"
	}
	if days <= 0 || days > 90 {
		days = 30
	}
	var q string
	switch metric {
	case "moisture":
		q = `
			SELECT tested_at::text, moisture_pct::float8, sample_business_id
			FROM qc_physical_tests
			WHERE moisture_pct IS NOT NULL
			  AND tested_at >= NOW() - ($1::int || ' days')::interval`
		if batchID != "" {
			q += ` AND batch_business_id = $2`
		}
		q += ` ORDER BY tested_at ASC`
	case "cup_score":
		q = `
			SELECT created_at::text, total_score::float8, sample_business_id
			FROM qc_cupping_sessions
			WHERE created_at >= NOW() - ($1::int || ' days')::interval`
		if batchID != "" {
			q += ` AND batch_business_id = $2`
		}
		q += ` ORDER BY created_at ASC`
	default:
		return SPCSeries{}, ErrBadInput
	}

	args := []any{days}
	if batchID != "" {
		args = append(args, batchID)
	}
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return SPCSeries{}, err
	}
	defer rows.Close()

	var values []float64
	var points []SPCPoint
	for rows.Next() {
		var ts, subgroup string
		var val float64
		if err := rows.Scan(&ts, &val, &subgroup); err != nil {
			return SPCSeries{}, err
		}
		values = append(values, val)
		points = append(points, SPCPoint{Timestamp: ts, Value: val, Subgroup: subgroup})
	}
	if err := rows.Err(); err != nil {
		return SPCSeries{}, err
	}

	series := SPCSeries{
		Metric:          metric,
		BatchBusinessID: batchID,
		Days:            days,
		Count:           len(values),
		Points:          points,
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
	}
	series.USL = opts.USL
	series.LSL = opts.LSL
	if opts.USL != nil && opts.LSL != nil && std > 0 {
		usl, lsl := *opts.USL, *opts.LSL
		cp := (usl - lsl) / (6 * std)
		cpkUpper := (usl - mean) / (3 * std)
		cpkLower := (mean - lsl) / (3 * std)
		cpk := cpkUpper
		if cpkLower < cpk {
			cpk = cpkLower
		}
		cpR := round2(cp)
		cpkR := round2(cpk)
		series.Cp = &cpR
		series.Cpk = &cpkR
	}
	return series, nil
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
