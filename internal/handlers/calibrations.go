package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"iag-quality-control/backend/internal/store"
)

// Instrument calibrations (migration 011). Events, not a register: there is no
// update verb, and history is read from the collection filtered by instrument
// rather than from a nested route under /instruments/:id — see router.go.

func (h *QC) ListCalibrations(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "200"))
	items, err := h.Store.ListInstrumentCalibrations(
		c.Request.Context(), c.Query("instrument"), c.Query("status"), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list calibrations"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

func (h *QC) GetCalibration(c *gin.Context) {
	item, err := h.Store.GetInstrumentCalibration(c.Request.Context(), c.Param("id"))
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not get calibration"})
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *QC) CreateCalibration(c *gin.Context) {
	var body struct {
		BusinessID     string         `json:"business_id"`
		InstrumentID   string         `json:"instrument_id"`
		InstrumentName string         `json:"instrument_name"`
		SerialRef      string         `json:"serial_ref"`
		CalDate        string         `json:"cal_date"`
		StandardRef    string         `json:"standard_ref"`
		PerformedBy    string         `json:"performed_by"`
		NextDue        string         `json:"next_due"`
		Result         string         `json:"result"`
		Status         string         `json:"status"`
		Notes          string         `json:"notes"`
		Attachments    string         `json:"attachments"`
		Attrs          map[string]any `json:"attrs"`
	}
	if err := bindJSONCoerced(c, &body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := h.Store.CreateInstrumentCalibration(c.Request.Context(), store.CreateCalibrationInput{
		BusinessID:     body.BusinessID,
		InstrumentID:   body.InstrumentID,
		InstrumentName: body.InstrumentName,
		SerialRef:      body.SerialRef,
		CalDate:        body.CalDate,
		StandardRef:    body.StandardRef,
		PerformedBy:    body.PerformedBy,
		NextDue:        body.NextDue,
		Result:         body.Result,
		Status:         body.Status,
		Notes:          body.Notes,
		Attachments:    body.Attachments,
		Attrs:          body.Attrs,
	})
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not record calibration"})
		return
	}
	c.JSON(http.StatusOK, item)
}
