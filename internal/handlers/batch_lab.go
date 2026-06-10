package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"iag-quality-control/backend/internal/store"
)

func (h *QC) GetBatchLab(c *gin.Context) {
	batchID := c.Param("batchId")
	summary, err := h.Store.GetBatchLabSummary(c.Request.Context(), batchID)
	hasSummary := err == nil
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load lab summary"})
		return
	}

	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
	samples, _ := h.Store.ListSamples(c.Request.Context(), "all", batchID, limit)
	physical, _ := h.Store.ListPhysicalTestsByBatch(c.Request.Context(), batchID, limit)
	chemical, _ := h.Store.ListChemicalTestsByBatch(c.Request.Context(), batchID, limit)
	cupping, _ := h.Store.ListCuppingByBatch(c.Request.Context(), batchID, limit)

	resp := gin.H{
		"batch_business_id": batchID,
		"samples":           samples,
		"physical_tests":    physical,
		"chemical_tests":    chemical,
		"cupping_sessions":  cupping,
	}
	if hasSummary {
		resp["summary"] = summary
	}
	c.JSON(http.StatusOK, resp)
}

func (h *QC) ListCoA(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	items, err := h.Store.ListCoA(c.Request.Context(), c.Query("lot_business_id"), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list coa"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

func (h *QC) DashboardSummary(c *gin.Context) {
	summary, err := h.Store.DashboardSummary(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "dashboard failed"})
		return
	}
	c.JSON(http.StatusOK, summary)
}
