package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"iag-quality-control/backend/internal/middleware"
	"iag-quality-control/backend/internal/store"
)

func (h *QC) ListPhysicalTests(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	items, err := h.Store.ListPhysicalTests(c.Request.Context(),
		c.Query("batch_business_id"), c.Query("sample_business_id"), c.Query("status"), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list physical tests"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

func (h *QC) ListChemicalTests(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	items, err := h.Store.ListChemicalTests(c.Request.Context(),
		c.Query("batch_business_id"), c.Query("sample_business_id"), c.Query("status"), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list chemical tests"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

func (h *QC) ListCuppingSessions(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	items, err := h.Store.ListCuppingSessions(c.Request.Context(),
		c.Query("batch_business_id"), c.Query("sample_business_id"), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list cupping sessions"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

func (h *QC) ListSampleCupping(c *gin.Context) {
	if _, err := h.Store.GetSample(c.Request.Context(), c.Param("id")); respondStoreErr(c, err) {
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	items, err := h.Store.ListCuppingBySample(c.Request.Context(), c.Param("id"), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list cupping"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

func (h *QC) GetSampleDetail(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("test_limit", "20"))
	detail, err := h.Store.GetSampleDetail(c.Request.Context(), c.Param("id"), limit)
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load sample detail"})
		return
	}
	c.JSON(http.StatusOK, detail)
}

func (h *QC) ListCustodyLogs(c *gin.Context) {
	if _, err := h.Store.GetSample(c.Request.Context(), c.Param("id")); respondStoreErr(c, err) {
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "30"))
	items, err := h.Store.ListCustodyLogs(c.Request.Context(), c.Param("id"), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list custody logs"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

func (h *QC) CreateCustodyLog(c *gin.Context) {
	var body struct {
		Action   string `json:"action" binding:"required"`
		Actor    string `json:"actor"`
		Location string `json:"location"`
		Notes    string `json:"notes"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := h.Store.CreateCustodyLog(c.Request.Context(), store.CreateCustodyLogInput{
		SampleBusinessID: c.Param("id"),
		Action:           body.Action,
		Actor:            body.Actor,
		Location:         body.Location,
		Notes:            body.Notes,
	})
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create custody log"})
		return
	}
	c.JSON(http.StatusCreated, item)
}

func (h *QC) InstrumentQueue(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "30"))
	items, err := h.Store.InstrumentQueue(c.Request.Context(), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "queue failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

func (h *QC) HPLCQueue(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "30"))
	items, err := h.Store.HPLCQueue(c.Request.Context(), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "queue failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

func (h *QC) CuppingQueue(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "30"))
	items, err := h.Store.CuppingQueue(c.Request.Context(), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "queue failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

func (h *QC) ListExternalAudits(c *gin.Context) {
	items, err := h.Store.ListExternalAudits(c.Request.Context(), c.Query("from"), c.Query("to"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list external audits"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

func (h *QC) UpsertExternalAudit(c *gin.Context) {
	var body struct {
		BusinessID  string `json:"business_id"`
		AuditType   string `json:"audit_type" binding:"required"`
		Body        string `json:"body"`
		Description string `json:"description"`
		AuditDate   string `json:"audit_date" binding:"required"`
		Status      string `json:"status"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := h.Store.UpsertExternalAudit(c.Request.Context(), store.UpsertExternalAuditInput{
		BusinessID: body.BusinessID, AuditType: body.AuditType, Body: body.Body,
		Description: body.Description, AuditDate: body.AuditDate, Status: body.Status,
	})
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not save external audit"})
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *QC) AuditPack(c *gin.Context) {
	ctx := c.Request.Context()
	compliance, _ := h.Store.ListComplianceLogs(ctx, 100)
	capas, _ := h.Store.ListCAPAs(ctx, "", 100)
	coas, _ := h.Store.ListCoA(ctx, "", 100)
	certs, _ := h.Store.ListPendingCertifications(ctx, 100)
	day, _ := h.Store.DayReport(ctx, c.Query("date"))
	c.JSON(http.StatusOK, gin.H{
		"generated_by": middleware.ActorLabel(c),
		"day_report":   day,
		"compliance":   compliance,
		"capas":        capas,
		"coas":         coas,
		"certifications_pending": certs,
	})
}
