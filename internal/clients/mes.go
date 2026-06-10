package clients

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type MES struct {
	baseClient
}

type ProductionRun struct {
	ID              string         `json:"id"`
	BusinessID      string         `json:"business_id"`
	BatchBusinessID string         `json:"batch_business_id"`
	Process         string         `json:"process"`
	Stage           string         `json:"stage"`
	StageIdx        int            `json:"stage_idx"`
	AssetTag        *string        `json:"asset_tag,omitempty"`
	Status          string         `json:"status"`
	StartedAt       time.Time      `json:"started_at"`
	CompletedAt     *time.Time     `json:"completed_at,omitempty"`
	Attrs           map[string]any `json:"attrs"`
}

type CCPReading struct {
	CCPCode    string    `json:"ccp_code"`
	Value      string    `json:"value"`
	Target     string    `json:"target"`
	Pass       bool      `json:"pass"`
	RecordedAt time.Time `json:"recorded_at"`
}

type TelemetryPoint struct {
	AssetTag   string    `json:"asset_tag"`
	Metric     string    `json:"metric"`
	Value      float64   `json:"value"`
	Unit       string    `json:"unit"`
	RecordedAt time.Time `json:"recorded_at"`
}

func NewMES(baseURL, tokenURL, clientID, clientSecret string) *MES {
	return &MES{baseClient: newBase(baseURL, tokenURL, clientID, clientSecret, "iag.mes")}
}

func (c *MES) Enabled() bool { return c != nil && c.enabled() }

func (c *MES) ListProductionRuns(ctx context.Context, status string, limit int) ([]ProductionRun, error) {
	if !c.Enabled() {
		return nil, fmt.Errorf("mes client disabled")
	}
	path := "/api/v1/production-runs"
	if status != "" {
		path += "?status=" + strings.TrimSpace(status)
	}
	var wrap struct {
		Items []ProductionRun `json:"items"`
	}
	if _, _, err := c.doJSON(ctx, http.MethodGet, path, nil, &wrap); err != nil {
		return nil, err
	}
	if limit > 0 && len(wrap.Items) > limit {
		wrap.Items = wrap.Items[:limit]
	}
	return wrap.Items, nil
}

func (c *MES) ListRunsByBatch(ctx context.Context, batchID string) ([]ProductionRun, error) {
	runs, err := c.ListProductionRuns(ctx, "", 200)
	if err != nil {
		return nil, err
	}
	batchID = strings.TrimSpace(batchID)
	var out []ProductionRun
	for _, r := range runs {
		if r.BatchBusinessID == batchID {
			out = append(out, r)
		}
	}
	return out, nil
}

func (c *MES) ListCCPReadings(ctx context.Context, runID string) ([]CCPReading, error) {
	if !c.Enabled() {
		return nil, fmt.Errorf("mes client disabled")
	}
	var wrap struct {
		Items []CCPReading `json:"items"`
	}
	path := fmt.Sprintf("/api/v1/production-runs/%s/ccp-readings", runID)
	if _, _, err := c.doJSON(ctx, http.MethodGet, path, nil, &wrap); err != nil {
		return nil, err
	}
	return wrap.Items, nil
}

func (c *MES) ListAssetTelemetry(ctx context.Context, assetTag string) ([]TelemetryPoint, error) {
	if !c.Enabled() {
		return nil, fmt.Errorf("mes client disabled")
	}
	assetTag = strings.TrimSpace(assetTag)
	if assetTag == "" {
		return nil, fmt.Errorf("asset tag required")
	}
	var wrap struct {
		Items []TelemetryPoint `json:"items"`
	}
	path := fmt.Sprintf("/api/v1/assets/%s/telemetry", assetTag)
	if _, _, err := c.doJSON(ctx, http.MethodGet, path, nil, &wrap); err != nil {
		return nil, err
	}
	return wrap.Items, nil
}

func (c *MES) ListAllTelemetry(ctx context.Context) ([]TelemetryPoint, error) {
	if !c.Enabled() {
		return nil, fmt.Errorf("mes client disabled")
	}
	var wrap struct {
		Items []TelemetryPoint `json:"items"`
	}
	if _, raw, err := c.doJSON(ctx, http.MethodGet, "/api/v1/telemetry/history?since="+time.Now().UTC().Add(-24*time.Hour).Format(time.RFC3339), nil, &wrap); err != nil {
		_ = raw
		// Fallback: no history endpoint access — return empty slice.
		return []TelemetryPoint{}, nil
	}
	return wrap.Items, nil
}

// PipelineBundle aggregates MES production context for a batch.
type PipelineBundle struct {
	Runs        []ProductionRun            `json:"runs"`
	CCPReadings map[string][]CCPReading    `json:"ccp_readings,omitempty"`
}

func (c *MES) BatchPipeline(ctx context.Context, batchID string) (PipelineBundle, error) {
	runs, err := c.ListRunsByBatch(ctx, batchID)
	if err != nil {
		return PipelineBundle{}, err
	}
	out := PipelineBundle{Runs: runs, CCPReadings: map[string][]CCPReading{}}
	for _, run := range runs {
		if run.Status != "running" && run.Status != "completed" {
			continue
		}
		readings, err := c.ListCCPReadings(ctx, run.ID)
		if err != nil {
			continue
		}
		if len(readings) > 0 {
			out.CCPReadings[run.BusinessID] = readings
		}
	}
	return out, nil
}

func (c *MES) UnmarshalRuns(raw json.RawMessage) ([]ProductionRun, error) {
	var runs []ProductionRun
	if len(raw) == 0 {
		return runs, nil
	}
	if err := json.Unmarshal(raw, &runs); err != nil {
		return nil, err
	}
	return runs, nil
}
