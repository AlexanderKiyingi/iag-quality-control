package store

import (
	"context"
	"strings"
)

type QueueItem struct {
	Sample         Sample   `json:"sample"`
	QueueKind      string   `json:"queue_kind"`
	PendingTests   []string `json:"pending_tests"`
	InstrumentHint string   `json:"instrument_hint,omitempty"`
}

func (s *Store) InstrumentQueue(ctx context.Context, limit int) ([]QueueItem, error) {
	return s.sampleQueue(ctx, limit, []string{"moisture", "screen", "defect"}, "physical", "Green lab instruments")
}

func (s *Store) HPLCQueue(ctx context.Context, limit int) ([]QueueItem, error) {
	return s.sampleQueue(ctx, limit, []string{"chlorogenic", "caffeine", "hplc", "water activity"}, "chemical", "Agilent HPLC")
}

func (s *Store) CuppingQueue(ctx context.Context, limit int) ([]QueueItem, error) {
	return s.sampleQueue(ctx, limit, []string{"cupping", "sca"}, "cupping", "Cupping lab")
}

func (s *Store) sampleQueue(ctx context.Context, limit int, keywords []string, kind, hint string) ([]QueueItem, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	samples, err := s.ListSamples(ctx, "all", "", 200)
	if err != nil {
		return nil, err
	}
	var out []QueueItem
	for _, sample := range samples {
		if sample.Status == "complete" {
			continue
		}
		done, err := s.sampleTestKindDone(ctx, sample.BusinessID, kind)
		if err != nil {
			return nil, err
		}
		if done {
			continue
		}
		pending := pendingTestsForQueue(sample, keywords)
		if len(pending) == 0 {
			continue
		}
		out = append(out, QueueItem{
			Sample:         sample,
			QueueKind:      kind,
			PendingTests:   pending,
			InstrumentHint: hint,
		})
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func pendingTestsForQueue(sample Sample, keywords []string) []string {
	var pending []string
	for _, test := range sample.TestsRequired {
		lower := strings.ToLower(test)
		for _, kw := range keywords {
			if strings.Contains(lower, kw) {
				pending = append(pending, test)
				break
			}
		}
	}
	return pending
}

func (s *Store) sampleTestKindDone(ctx context.Context, sampleID, kind string) (bool, error) {
	var n int
	var q string
	switch kind {
	case "physical":
		q = `SELECT COUNT(*)::int FROM qc_physical_tests WHERE sample_business_id = $1`
	case "chemical":
		q = `SELECT COUNT(*)::int FROM qc_chemical_tests WHERE sample_business_id = $1`
	case "cupping":
		q = `SELECT COUNT(*)::int FROM qc_cupping_sessions WHERE sample_business_id = $1`
	default:
		return false, ErrBadInput
	}
	if err := s.pool.QueryRow(ctx, q, sampleID).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}
