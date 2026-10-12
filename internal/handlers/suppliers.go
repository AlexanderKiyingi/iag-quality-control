package handlers

import (
	"context"
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"iag-quality-control/backend/internal/store"
)

/*
refreshSupplier recomputes a supplier's scorecard after anything that moves it
— an inspection, a SCAR — and announces qc.supplier.scorecard when quality's
recommended status changed. Procurement owns the decision; this is the
evidence it decides on. Never fails the write that triggered it.
*/
func (h *QC) refreshSupplier(ctx context.Context, supplier string) {
	if supplier == "" {
		return
	}
	cards, err := h.Store.SupplierScorecards(ctx, supplier, 365)
	if err != nil || len(cards) == 0 {
		if err != nil {
			log.Printf("quality-control: scorecard for %s: %v", supplier, err)
		}
		return
	}
	card := cards[0]
	changed, err := h.Store.RecordSupplierRecommendation(ctx, card)
	if err != nil {
		log.Printf("quality-control: record recommendation for %s: %v", supplier, err)
		return
	}
	if !changed {
		return
	}
	payload := map[string]any{
		"supplier_ref":       card.SupplierRef,
		"recommended_status": card.Recommendation,
		"reasons":            card.Reasons,
		"lots_decided":       card.LotsDecided,
		"lots_accepted":      card.LotsAccepted,
		"open_scars":         card.OpenSCARs,
		"overdue_scars":      card.OverdueSCARs,
		"window_days":        card.Days,
	}
	if card.AcceptanceRate != nil {
		payload["acceptance_rate"] = *card.AcceptanceRate
	}
	if err := h.publish(ctx, "qc.supplier.scorecard", payload); err != nil {
		log.Printf("quality-control: publish scorecard for %s: %v", supplier, err)
	}
}

func (h *QC) SupplierScorecards(c *gin.Context) {
	days, _ := strconv.Atoi(c.DefaultQuery("days", "365"))
	items, err := h.Store.SupplierScorecards(c.Request.Context(), c.Query("supplier_ref"), days)
	if err != nil {
		log.Printf("quality-control: supplier scorecards: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not build supplier scorecards"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

func (h *QC) ListSCARs(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "200"))
	items, err := h.Store.ListSCARs(c.Request.Context(), c.Query("supplier_ref"), c.Query("status"), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list SCARs"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

func (h *QC) GetSCAR(c *gin.Context) {
	item, err := h.Store.GetSCAR(c.Request.Context(), c.Param("id"))
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load SCAR"})
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *QC) CreateSCAR(c *gin.Context) {
	var body store.SCARInput
	if err := bindJSONCoerced(c, &body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	body.Actor = actorOf(c)
	item, err := h.Store.CreateSCAR(c.Request.Context(), body)
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not raise SCAR"})
		return
	}
	h.refreshSupplier(c.Request.Context(), item.SupplierRef)
	c.JSON(http.StatusCreated, item)
}

// UpdateSCAR records the supplier's response, the effectiveness check, or a
// status short of closed; closing is a signature (POST /signatures).
func (h *QC) UpdateSCAR(c *gin.Context) {
	var body store.SCARInput
	if err := bindJSONCoerced(c, &body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	body.Actor = actorOf(c)
	item, err := h.Store.UpdateSCAR(c.Request.Context(), c.Param("id"), body)
	if err == store.ErrSignatureState {
		respondSignatureRequired(c, "closing a SCAR is an electronic signature — sign it with meaning \"closed\"")
		return
	}
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not update SCAR"})
		return
	}
	h.refreshSupplier(c.Request.Context(), item.SupplierRef)
	c.JSON(http.StatusOK, item)
}
