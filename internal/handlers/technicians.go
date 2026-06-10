package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"iag-quality-control/backend/internal/store"
)

func (h *QC) ListTechnicians(c *gin.Context) {
	activeOnly := c.Query("active") != "false"
	items, err := h.Store.ListTechnicians(c.Request.Context(), activeOnly)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list technicians"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

func (h *QC) GetTechnician(c *gin.Context) {
	item, err := h.Store.GetTechnician(c.Request.Context(), c.Param("id"))
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load technician"})
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *QC) UpsertTechnician(c *gin.Context) {
	var body struct {
		BusinessID     string   `json:"business_id" binding:"required"`
		Name           string   `json:"name" binding:"required"`
		Role           string   `json:"role"`
		Level          string   `json:"level"`
		Color          string   `json:"color"`
		Certifications []string `json:"certifications"`
		Active         *bool    `json:"active"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := h.Store.UpsertTechnician(c.Request.Context(), store.UpsertTechnicianInput{
		BusinessID: body.BusinessID, Name: body.Name, Role: body.Role, Level: body.Level,
		Color: body.Color, Certifications: body.Certifications, Active: body.Active,
	})
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not save technician"})
		return
	}
	c.JSON(http.StatusOK, item)
}
