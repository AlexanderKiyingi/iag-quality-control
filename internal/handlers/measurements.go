package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"iag-quality-control/backend/internal/store"
)

// Parameterised lab measurements (migration 013).
//
// Listing is a collection so a results screen can read them all; recording is
// nested under the sample, matching physical-tests, chemical-tests and cupping —
// a measurement that names no sample cannot be traced to a batch.

func (h *QC) ListLabMeasurements(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "200"))
	items, err := h.Store.ListLabMeasurements(c.Request.Context(), c.Query("sample"), c.Query("parameter"), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list lab measurements"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

func (h *QC) PostLabMeasurement(c *gin.Context) {
	var body struct {
		BusinessID  string         `json:"business_id"`
		Parameter   string         `json:"parameter"`
		Value       string         `json:"value"`
		Unit        string         `json:"unit"`
		SpecLimit   string         `json:"spec_limit"`
		MethodID    string         `json:"method_id"`
		Analyst     string         `json:"analyst"`
		Result      string         `json:"result"`
		Status      string         `json:"status"`
		Notes       string         `json:"notes"`
		Attachments string         `json:"attachments"`
		ReportedAt  string         `json:"reported_at"`
		Attrs       map[string]any `json:"attrs"`
	}
	// Value stays a string all the way down: plenty of real results are "<0.1"
	// or "pass", and the store fills value_num only when the text parses.
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := h.Store.CreateLabMeasurement(c.Request.Context(), store.CreateLabMeasurementInput{
		BusinessID:       body.BusinessID,
		SampleBusinessID: c.Param("id"),
		Parameter:        body.Parameter,
		Value:            body.Value,
		Unit:             body.Unit,
		SpecLimit:        body.SpecLimit,
		MethodID:         body.MethodID,
		Analyst:          body.Analyst,
		Result:           body.Result,
		Status:           body.Status,
		Notes:            body.Notes,
		Attachments:      body.Attachments,
		ReportedAt:       body.ReportedAt,
		Attrs:            body.Attrs,
	})
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not record lab measurement"})
		return
	}
	c.JSON(http.StatusOK, item)
}
