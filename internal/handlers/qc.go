package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"iag-quality-control/backend/internal/events"
)

type QC struct {
	Pub *events.Publisher
}

func (h *QC) PostSample(c *gin.Context) {
	var body struct {
		BatchBusinessID string `json:"batch_business_id" binding:"required"`
		SampleID        string `json:"sample_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	data := map[string]any{
		"batch_business_id": body.BatchBusinessID,
		"sample_id":         body.SampleID,
	}
	if err := h.Pub.Publish(c.Request.Context(), "qc.sample.submitted", data); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "kafka publish failed"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"status": "published", "event_type": "qc.sample.submitted"})
}

func (h *QC) PostLabResult(c *gin.Context) {
	var body struct {
		BatchBusinessID string  `json:"batch_business_id" binding:"required"`
		Moisture        float64 `json:"moisture"`
		CupScore        float64 `json:"cup_score"`
		Grade           string  `json:"grade"`
		Tester          string  `json:"tester"`
		Defects         float64 `json:"defects"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	data := map[string]any{
		"batch_business_id": body.BatchBusinessID,
		"moisture":          body.Moisture,
		"cup_score":         body.CupScore,
		"grade":             body.Grade,
		"tester":            body.Tester,
	}
	if body.Defects > 0 {
		data["defects"] = body.Defects
	}
	if err := h.Pub.Publish(c.Request.Context(), "qc.lab.result_recorded", data); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "kafka publish failed"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"status": "published", "event_type": "qc.lab.result_recorded"})
}

func (h *QC) PostCoA(c *gin.Context) {
	var body struct {
		LotBusinessID string `json:"lot_business_id" binding:"required"`
		CoaNumber     string `json:"coa_number" binding:"required"`
		DocumentRef   string `json:"document_ref"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	data := map[string]any{
		"lot_business_id": body.LotBusinessID,
		"coa_number":      body.CoaNumber,
		"document_ref":    body.DocumentRef,
	}
	if err := h.Pub.Publish(c.Request.Context(), "qc.coa.issued", data); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "kafka publish failed"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"status": "published", "event_type": "qc.coa.issued"})
}
