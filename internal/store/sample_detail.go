package store

import (
	"context"
)

type SampleDetail struct {
	Sample         Sample           `json:"sample"`
	PhysicalTests  []PhysicalTest   `json:"physical_tests"`
	ChemicalTests  []ChemicalTest   `json:"chemical_tests"`
	CuppingSessions []CuppingSession `json:"cupping_sessions"`
	CustodyLogs    []CustodyLog     `json:"custody_logs"`
	LabSummary     *BatchLabSummary `json:"lab_summary,omitempty"`
}

func (s *Store) GetSampleDetail(ctx context.Context, sampleID string, testLimit int) (SampleDetail, error) {
	sample, err := s.GetSample(ctx, sampleID)
	if err != nil {
		return SampleDetail{}, err
	}
	if testLimit <= 0 || testLimit > 50 {
		testLimit = 20
	}
	physical, _ := s.ListPhysicalTestsBySample(ctx, sampleID, testLimit)
	chemical, _ := s.ListChemicalTestsBySample(ctx, sampleID, testLimit)
	cupping, _ := s.ListCuppingBySample(ctx, sampleID, testLimit)
	custody, _ := s.ListCustodyLogs(ctx, sampleID, testLimit)

	out := SampleDetail{
		Sample:          sample,
		PhysicalTests:   physical,
		ChemicalTests:   chemical,
		CuppingSessions: cupping,
		CustodyLogs:     custody,
	}
	if summary, err := s.GetBatchLabSummary(ctx, sample.BatchBusinessID); err == nil {
		out.LabSummary = &summary
	}
	return out, nil
}
