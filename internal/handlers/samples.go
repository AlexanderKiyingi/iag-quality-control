package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"iag-quality-control/backend/internal/store"
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

// PatchSample edits a sample. It used to bind {status} only, so a client that
// wanted to change the priority or the technician had nowhere to send it —
// the Lab app packed those into notes as JSON. Every field is optional and
// nil leaves the column alone; a body with nothing set is a 400.
func (h *QC) PatchSample(c *gin.Context) {
	var body struct {
		Status       *string `json:"status"`
		SampleType   *string `json:"sample_type"`
		Priority     *string `json:"priority"`
		AssignedTech *string `json:"assigned_tech"`
		Notes        *string `json:"notes"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	sample, err := h.Store.UpdateSample(c.Request.Context(), c.Param("id"), store.SamplePatch{
		Status: body.Status, SampleType: body.SampleType, Priority: body.Priority,
		AssignedTech: body.AssignedTech, Notes: body.Notes,
	})
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not update sample"})
		return
	}
	c.JSON(http.StatusOK, sample)
}
