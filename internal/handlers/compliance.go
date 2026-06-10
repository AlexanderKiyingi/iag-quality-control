package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"iag-quality-control/backend/internal/store"
)

func (h *QC) ListComplianceLogs(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	items, err := h.Store.ListComplianceLogs(c.Request.Context(), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list compliance logs"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

func (h *QC) CreateComplianceLog(c *gin.Context) {
	var body struct {
		LogType       string `json:"log_type"`
		CCP           string `json:"ccp"`
		LoggedDate    string `json:"logged_date"`
		LoggedTime    string `json:"logged_time"`
		TechID        string `json:"tech_id"`
		MeasuredValue string `json:"measured_value"`
		LimitValue    string `json:"limit_value"`
		Status        string `json:"status"`
		ActionTaken   string `json:"action_taken"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := h.Store.CreateComplianceLog(c.Request.Context(), store.CreateComplianceLogInput{
		LogType: body.LogType, CCP: body.CCP, LoggedDate: body.LoggedDate, LoggedTime: body.LoggedTime,
		TechID: body.TechID, MeasuredValue: body.MeasuredValue, LimitValue: body.LimitValue,
		Status: body.Status, ActionTaken: body.ActionTaken,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create compliance log"})
		return
	}
	c.JSON(http.StatusCreated, item)
}

func (h *QC) ListCAPAs(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	items, err := h.Store.ListCAPAs(c.Request.Context(), c.DefaultQuery("status", "all"), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list capas"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

func (h *QC) UpsertCAPA(c *gin.Context) {
	var body struct {
		BusinessID       string  `json:"business_id"`
		Title            string  `json:"title" binding:"required"`
		SourceRef        string  `json:"source_ref"`
		Status           string  `json:"status"`
		Priority         string  `json:"priority"`
		Owner            string  `json:"owner"`
		RootCause        string  `json:"root_cause"`
		CorrectiveAction string  `json:"corrective_action"`
		OpenedAt         string  `json:"opened_at"`
		ClosedAt         *string `json:"closed_at"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := h.Store.UpsertCAPA(c.Request.Context(), store.UpsertCAPAInput{
		BusinessID: body.BusinessID, Title: body.Title, SourceRef: body.SourceRef, Status: body.Status,
		Priority: body.Priority, Owner: body.Owner, RootCause: body.RootCause,
		CorrectiveAction: body.CorrectiveAction, OpenedAt: body.OpenedAt, ClosedAt: body.ClosedAt,
	})
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not save capa"})
		return
	}
	c.JSON(http.StatusOK, item)
}
