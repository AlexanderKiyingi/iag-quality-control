package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"iag-quality-control/backend/internal/jobs"
	"iag-quality-control/backend/internal/store"
)

func (h *QC) ListInstruments(c *gin.Context) {
	items, err := h.Store.ListInstruments(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list instruments"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

func (h *QC) GetInstrument(c *gin.Context) {
	item, err := h.Store.GetInstrument(c.Request.Context(), c.Param("id"))
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load instrument"})
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *QC) UpsertInstrument(c *gin.Context) {
	var body struct {
		BusinessID     string  `json:"business_id"`
		Name           string  `json:"name" binding:"required"`
		InstrumentType string  `json:"instrument_type"`
		Location       string  `json:"location"`
		Status         string  `json:"status"`
		OwnerTech      string  `json:"owner_tech"`
		LastCalDate    *string `json:"last_cal_date"`
		NextCalDate    *string `json:"next_cal_date"`
		Note           string  `json:"note"`
		Samples24h     int     `json:"samples_24h"`
		MESAssetTag    string  `json:"mes_asset_tag"`
	}
	if err := bindJSONCoerced(c, &body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := h.Store.UpsertInstrument(c.Request.Context(), store.UpsertInstrumentInput{
		BusinessID:     body.BusinessID,
		Name:           body.Name,
		InstrumentType: body.InstrumentType,
		Location:       body.Location,
		Status:         body.Status,
		OwnerTech:      body.OwnerTech,
		LastCalDate:    body.LastCalDate,
		NextCalDate:    body.NextCalDate,
		Note:           body.Note,
		Samples24h:     body.Samples24h,
		MESAssetTag:    body.MESAssetTag,
	})
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not save instrument"})
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *QC) SyncInstruments(c *gin.Context) {
	if h.MES == nil || !h.MES.Enabled() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "mes integration disabled"})
		return
	}
	n, err := jobs.SyncInstrumentsFromMES(c.Request.Context(), h.Store, h.MES)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "instrument sync failed", "detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"updated": n})
}
