package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

func (h *QC) ListSamples(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	status := c.DefaultQuery("status", "all")
	batchID := c.Query("batch_business_id")
	items, err := h.Store.ListSamples(c.Request.Context(), status, batchID, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list samples"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

func (h *QC) GetSample(c *gin.Context) {
	sample, err := h.Store.GetSample(c.Request.Context(), c.Param("id"))
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not get sample"})
		return
	}
	c.JSON(http.StatusOK, sample)
}

func (h *QC) PatchSample(c *gin.Context) {
	var body struct {
		Status string `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	sample, err := h.Store.UpdateSampleStatus(c.Request.Context(), c.Param("id"), body.Status)
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not update sample"})
		return
	}
	c.JSON(http.StatusOK, sample)
}
