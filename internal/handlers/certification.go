package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"iag-quality-control/backend/internal/events"
	"iag-quality-control/backend/internal/middleware"
	"iag-quality-control/backend/internal/store"
)

func (h *QC) CreateCertificationRequest(c *gin.Context) {
	var body struct {
		BatchBusinessID string `json:"batch_business_id" binding:"required"`
		LotBusinessID   string `json:"lot_business_id"`
		CoaNumber       string `json:"coa_number"`
		DocumentRef     string `json:"document_ref"`
		Notes           string `json:"notes"`
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
	if body.LotBusinessID != "" {
		if err := h.validateExportLot(c.Request.Context(), body.LotBusinessID); err != nil {
			if respondStoreErr(c, err) {
				return
			}
			c.JSON(http.StatusBadGateway, gin.H{"error": "scm validation failed"})
			return
		}
	}
	req, err := h.Store.CreateCertificationRequest(c.Request.Context(), store.CreateCertificationInput{
		BatchBusinessID: body.BatchBusinessID,
		LotBusinessID:   body.LotBusinessID,
		CoaNumber:       body.CoaNumber,
		DocumentRef:     body.DocumentRef,
		RequestedBy:     middleware.ActorLabel(c),
		Notes:           body.Notes,
	})
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create certification request"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"request": req})
}

func (h *QC) ListPendingCertifications(c *gin.Context) {
	items, err := h.Store.ListPendingCertifications(c.Request.Context(), 50)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list certifications"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

func (h *QC) GetCertificationRequest(c *gin.Context) {
	req, err := h.Store.GetCertificationRequest(c.Request.Context(), c.Param("id"))
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load certification"})
		return
	}
	c.JSON(http.StatusOK, req)
}

func (h *QC) ApproveCertification(c *gin.Context) {
	var body struct {
		Decision string `json:"decision" binding:"required"`
		Notes    string `json:"notes"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req, coa, issued, err := h.Store.ApproveCertificationStage(c.Request.Context(), store.ApproveCertificationInput{
		RequestBusinessID: c.Param("id"),
		Approver:          middleware.ActorLabel(c),
		Decision:          body.Decision,
		Notes:             body.Notes,
	})
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "approval failed"})
		return
	}
	resp := gin.H{"request": req}
	if issued {
		data := map[string]any{
			"lot_business_id":   coa.LotBusinessID,
			"coa_number":        coa.CoaNumber,
			"document_ref":      coa.DocumentRef,
			"batch_business_id": coa.BatchBusinessID,
		}
		if err := h.publish(c.Request.Context(), "qc.coa.issued", data); err != nil {
			if errors.Is(err, events.ErrDisabled) {
				c.JSON(http.StatusServiceUnavailable, gin.H{"error": "kafka unavailable", "request": req, "coa": coa})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "kafka publish failed"})
			return
		}
		resp["coa"] = coa
		resp["event_type"] = "qc.coa.issued"
	}
	c.JSON(http.StatusOK, resp)
}
