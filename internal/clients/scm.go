package clients

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type SCM struct {
	baseClient
}

type ListMeta struct {
	Page       int  `json:"page"`
	Limit      int  `json:"limit"`
	Total      int  `json:"total"`
	TotalPages int  `json:"total_pages"`
	HasMore    bool `json:"has_more"`
}

type SCMListResult struct {
	Data json.RawMessage `json:"data"`
	Meta *ListMeta       `json:"meta,omitempty"`
}

func NewSCM(baseURL, tokenURL, clientID, clientSecret string) *SCM {
	return &SCM{baseClient: newBase(baseURL, tokenURL, clientID, clientSecret, "iag.supply-chain")}
}

func (c *SCM) Enabled() bool { return c != nil && c.enabled() }

func (c *SCM) get(ctx context.Context, path string, out any) (*ListMeta, error) {
	if !c.Enabled() {
		return nil, fmt.Errorf("scm client disabled")
	}
	var wrap SCMListResult
	if _, _, err := c.doJSON(ctx, http.MethodGet, path, nil, &wrap); err != nil {
		return nil, err
	}
	if out != nil && len(wrap.Data) > 0 && string(wrap.Data) != "null" {
		if err := json.Unmarshal(wrap.Data, out); err != nil {
			return nil, err
		}
	}
	return wrap.Meta, nil
}

func (c *SCM) ListBatches(ctx context.Context, stage, bean string, page, limit int) (json.RawMessage, *ListMeta, error) {
	q := url.Values{}
	if stage != "" {
		q.Set("stage", stage)
	}
	if bean != "" {
		q.Set("bean", bean)
	}
	if page > 0 {
		q.Set("page", fmt.Sprintf("%d", page))
	}
	if limit > 0 {
		q.Set("limit", fmt.Sprintf("%d", limit))
	}
	path := "/api/v1/batches"
	if enc := q.Encode(); enc != "" {
		path += "?" + enc
	}
	var raw json.RawMessage
	meta, err := c.get(ctx, path, &raw)
	return raw, meta, err
}

func (c *SCM) GetBatch(ctx context.Context, businessID string) (json.RawMessage, error) {
	var raw json.RawMessage
	_, err := c.get(ctx, "/api/v1/batches/"+url.PathEscape(businessID), &raw)
	return raw, err
}

func (c *SCM) ListExportLots(ctx context.Context, page, limit int) (json.RawMessage, *ListMeta, error) {
	q := url.Values{}
	if page > 0 {
		q.Set("page", fmt.Sprintf("%d", page))
	}
	if limit > 0 {
		q.Set("limit", fmt.Sprintf("%d", limit))
	}
	path := "/api/v1/export-lots"
	if enc := q.Encode(); enc != "" {
		path += "?" + enc
	}
	var raw json.RawMessage
	meta, err := c.get(ctx, path, &raw)
	return raw, meta, err
}

func (c *SCM) GetExportLot(ctx context.Context, businessID string) (json.RawMessage, error) {
	var raw json.RawMessage
	_, err := c.get(ctx, "/api/v1/export-lots/"+url.PathEscape(businessID), &raw)
	return raw, err
}

func (c *SCM) ListFarmers(ctx context.Context, search, bean, cert string, page, limit int) (json.RawMessage, *ListMeta, error) {
	q := url.Values{}
	if search != "" {
		q.Set("search", search)
	}
	if bean != "" {
		q.Set("bean", bean)
	}
	if cert != "" {
		q.Set("cert", cert)
	}
	if page > 0 {
		q.Set("page", fmt.Sprintf("%d", page))
	}
	if limit > 0 {
		q.Set("limit", fmt.Sprintf("%d", limit))
	}
	path := "/api/v1/farmers"
	if enc := q.Encode(); enc != "" {
		path += "?" + enc
	}
	var raw json.RawMessage
	meta, err := c.get(ctx, path, &raw)
	return raw, meta, err
}

func (c *SCM) GetFarmer(ctx context.Context, businessID string) (json.RawMessage, error) {
	var raw json.RawMessage
	_, err := c.get(ctx, "/api/v1/farmers/"+url.PathEscape(businessID), &raw)
	return raw, err
}

func (c *SCM) ValidateBatch(ctx context.Context, batchBusinessID string) (bool, error) {
	if !c.Enabled() {
		return false, fmt.Errorf("scm client disabled")
	}
	_, err := c.GetBatch(ctx, batchBusinessID)
	if err != nil {
		if strings.Contains(err.Error(), "404") || strings.Contains(strings.ToLower(err.Error()), "not_found") {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (c *SCM) ValidateExportLot(ctx context.Context, lotBusinessID string) (bool, error) {
	if !c.Enabled() {
		return false, fmt.Errorf("scm client disabled")
	}
	_, err := c.GetExportLot(ctx, lotBusinessID)
	if err != nil {
		if strings.Contains(err.Error(), "404") || strings.Contains(strings.ToLower(err.Error()), "not_found") {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
