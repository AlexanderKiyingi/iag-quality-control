package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"iag-quality-control/backend/internal/store"
)

// Stability studies (migration 011). Upserted by business_id like the other
// registers, so a form save is idempotent and omitted columns are preserved.

func (h *QC) ListStabilityStudies(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "200"))
	items, err := h.Store.ListStabilityStudies(c.Request.Context(), c.Query("status"), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list stability studies"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

func (h *QC) UpsertStabilityStudy(c *gin.Context) {
	var body struct {
		BusinessID       string         `json:"business_id"`
		Product          string         `json:"product"`
		StartDate        *string        `json:"start_date"`
		StorageCondition *string        `json:"storage_condition"`
		DurationDays     *int           `json:"duration_days"`
		NextPullDate     *string        `json:"next_pull_date"`
		TestsScheduled   *string        `json:"tests_scheduled"`
		Owner            *string        `json:"owner"`
		Status           *string        `json:"status"`
		Notes            *string        `json:"notes"`
		Attachments      *string        `json:"attachments"`
		Attrs            map[string]any `json:"attrs"`
	}
	if err := bindJSONCoerced(c, &body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := h.Store.UpsertStabilityStudy(c.Request.Context(), store.UpsertStabilityStudyInput{
		BusinessID:       body.BusinessID,
		Product:          body.Product,
		StartDate:        body.StartDate,
		StorageCondition: body.StorageCondition,
		DurationDays:     body.DurationDays,
		NextPullDate:     body.NextPullDate,
		TestsScheduled:   body.TestsScheduled,
		Owner:            body.Owner,
		Status:           body.Status,
		Notes:            body.Notes,
		Attachments:      body.Attachments,
		Attrs:            body.Attrs,
	})
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not save stability study"})
		return
	}
	c.JSON(http.StatusOK, item)
}
