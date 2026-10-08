package handlers

import (
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"iag-quality-control/backend/internal/store"
)

// Incoming inspections (migration 015). A supplier lot checked on arrival.
// Reuses the compliance permissions — it is the same officer's register.

func (h *QC) ListIncomingInspections(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "200"))
	items, err := h.Store.ListIncomingInspections(c.Request.Context(), c.Query("lot"), c.Query("status"), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list incoming inspections"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

func (h *QC) UpsertIncomingInspection(c *gin.Context) {
	var body struct {
		BusinessID  string         `json:"business_id"`
		SourceLot   string         `json:"source_lot"`
		InspectedAt *string        `json:"inspected_at"`
		ItemRef     *string        `json:"item_ref"`
		SampleSize  *int           `json:"sample_size"`
		MoisturePct *float64       `json:"moisture_pct"`
		DefectCount *int           `json:"defect_count"`
		Inspector   *string        `json:"inspector"`
		Result      *string        `json:"result"`
		Status      *string        `json:"status"`
		Notes       *string        `json:"notes"`
		Attachments *string        `json:"attachments"`
		Attrs       map[string]any `json:"attrs"`
		// 016: why the inspector's result disagrees with the verdict.
		OverrideReason *string `json:"override_reason"`
	}
	// Coerced: sample_size, defect_count and moisture arrive as strings from a
	// web form.
	if err := bindJSONCoerced(c, &body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := h.Store.UpsertIncomingInspection(c.Request.Context(), store.UpsertIncomingInspectionInput{
		BusinessID: body.BusinessID, SourceLot: body.SourceLot, InspectedAt: body.InspectedAt,
		ItemRef: body.ItemRef, SampleSize: body.SampleSize, MoisturePct: body.MoisturePct,
		DefectCount: body.DefectCount, Inspector: body.Inspector, Result: body.Result,
		Status: body.Status, Notes: body.Notes, Attachments: body.Attachments, Attrs: body.Attrs,
		OverrideReason: body.OverrideReason, Actor: actorOf(c),
	})
	if errors.Is(err, store.ErrAutoActions) {
		log.Printf("quality-control: incoming inspection %s: %v", item.BusinessID, err)
		err = nil
	}
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not save incoming inspection"})
		return
	}
	h.announceAutoActions(c.Request.Context(), item.AutoActions, item.BusinessID)
	c.JSON(http.StatusOK, item)
}
