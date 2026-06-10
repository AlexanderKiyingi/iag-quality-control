package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"iag-quality-control/backend/internal/events"
	"iag-quality-control/backend/internal/labels"
	"iag-quality-control/backend/internal/middleware"
	"iag-quality-control/backend/internal/store"
)

func (h *QC) PostSample(c *gin.Context) {
	var body struct {
		BatchBusinessID string   `json:"batch_business_id" binding:"required"`
		SampleID        string   `json:"sample_id"`
		SampleType      string   `json:"sample_type"`
		Priority        string   `json:"priority"`
		AssignedTech    string   `json:"assigned_tech"`
		TestsRequired   []string `json:"tests_required"`
		Notes           string   `json:"notes"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.validateBatch(c.Request.Context(), body.BatchBusinessID); err != nil {
		if respondStoreErr(c, err) {
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": "scm validation failed"})
		return
	}
	sample, err := h.Store.CreateSample(c.Request.Context(), store.CreateSampleInput{
		BatchBusinessID: body.BatchBusinessID,
		SampleID:        body.SampleID,
		SampleType:      body.SampleType,
		Priority:        body.Priority,
		AssignedTech:    body.AssignedTech,
		TestsRequired:   body.TestsRequired,
		Notes:           body.Notes,
	})
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create sample"})
		return
	}
	data := map[string]any{
		"batch_business_id": sample.BatchBusinessID,
		"sample_id":         sample.BusinessID,
		"sample_type":       sample.SampleType,
		"tests_required":    sample.TestsRequired,
	}
	if err := h.publish(c.Request.Context(), "qc.sample.submitted", data); err != nil {
		if errors.Is(err, events.ErrDisabled) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "kafka unavailable"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "kafka publish failed"})
		return
	}
	label := labels.NewSampleLabel(sample.BusinessID, sample.BatchBusinessID, sample.SampleType)
	c.JSON(http.StatusCreated, gin.H{
		"status":        "created",
		"event_type":    "qc.sample.submitted",
		"sample":        sample,
		"barcode_value": label.BarcodeValue,
		"label":         label,
	})
}

func (h *QC) PostLabResult(c *gin.Context) {
	var body struct {
		BatchBusinessID string  `json:"batch_business_id" binding:"required"`
		Moisture        float64 `json:"moisture"`
		CupScore        float64 `json:"cup_score"`
		Grade           string  `json:"grade"`
		Tester          string  `json:"tester"`
		Defects         float64 `json:"defects"`
		WaterActivity   float64 `json:"water_activity"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	summary, err := h.Store.RecordLabResult(c.Request.Context(), store.RecordLabResultInput{
		BatchBusinessID: body.BatchBusinessID,
		Moisture:        body.Moisture,
		CupScore:        body.CupScore,
		Grade:           body.Grade,
		Tester:          body.Tester,
		Defects:         body.Defects,
		WaterActivity:   body.WaterActivity,
	})
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not record lab result"})
		return
	}
	data := labResultPayload(summary)
	if err := h.publish(c.Request.Context(), "qc.lab.result_recorded", data); err != nil {
		if errors.Is(err, events.ErrDisabled) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "kafka unavailable"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "kafka publish failed"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"status":     "recorded",
		"event_type": "qc.lab.result_recorded",
		"summary":    summary,
	})
}

func (h *QC) PostCoA(c *gin.Context) {
	var body struct {
		LotBusinessID   string `json:"lot_business_id" binding:"required"`
		BatchBusinessID string `json:"batch_business_id"`
		CoaNumber     string `json:"coa_number" binding:"required"`
		DocumentRef   string `json:"document_ref"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.validateExportLot(c.Request.Context(), body.LotBusinessID); err != nil {
		if respondStoreErr(c, err) {
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": "scm validation failed"})
		return
	}
	coa, err := h.Store.CreateCoA(c.Request.Context(), store.CreateCoAInput{
		LotBusinessID:   body.LotBusinessID,
		BatchBusinessID: body.BatchBusinessID,
		CoaNumber:     body.CoaNumber,
		DocumentRef:   body.DocumentRef,
		IssuedBy:        middleware.ActorLabel(c),
	})
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not issue coa"})
		return
	}
	data := map[string]any{
		"lot_business_id": coa.LotBusinessID,
		"coa_number":      coa.CoaNumber,
		"document_ref":    coa.DocumentRef,
	}
	if coa.BatchBusinessID != "" {
		data["batch_business_id"] = coa.BatchBusinessID
	}
	if err := h.publish(c.Request.Context(), "qc.coa.issued", data); err != nil {
		if errors.Is(err, events.ErrDisabled) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "kafka unavailable"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "kafka publish failed"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"status":     "issued",
		"event_type": "qc.coa.issued",
		"coa":        coa,
	})
}
