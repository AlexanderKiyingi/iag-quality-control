package handlers

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"iag-quality-control/backend/internal/store"
)

/*
checkSPC tests a just-recorded reading against its parameters' control
charts (022) and sends a qc.alert for each rule it broke. It never fails the
write: the reading is recorded either way, and a broken chart check is
logged, not returned.
*/
func (h *QC) checkSPC(ctx context.Context, ref, actor string, parameters ...string) []store.SPCAlert {
	alerts, err := h.Store.CheckSPCAlarms(ctx, ref, parameters, actor)
	if err != nil {
		log.Printf("quality-control: spc check for %s: %v", ref, err)
	}
	for _, a := range alerts {
		body := fmt.Sprintf("%s = %g on %s (batch %s) broke rule %d: %s. Centre %g, control limits %g–%g.",
			a.Parameter, a.Value, a.Ref, a.BatchBusinessID, a.Rule, a.RuleName, a.Center, a.LCL, a.UCL)
		if a.NonConformance != "" {
			body += " Non-conformance " + a.NonConformance + " raised."
		}
		h.notifyAlert(ctx, "spc", "qc.alert", "Out of control: "+a.Parameter, body)
	}
	return alerts
}

func (h *QC) ListSPCAlerts(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "200"))
	items, err := h.Store.ListSPCAlerts(c.Request.Context(), c.Query("status"), c.Query("parameter"), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list spc alerts"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

// AcknowledgeSPCAlert closes an alarm with what was found.
func (h *QC) AcknowledgeSPCAlert(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "alert id is a number"})
		return
	}
	var body struct {
		Note string `json:"note"`
	}
	_ = c.ShouldBindJSON(&body)
	err = h.Store.AcknowledgeSPCAlert(c.Request.Context(), id, actorOf(c), body.Note)
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not acknowledge spc alert"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id, "status": "acknowledged"})
}

func (h *QC) GetSPCSettings(c *gin.Context) {
	item, err := h.Store.GetSPCSettings(c.Request.Context(), c.Param("parameter"))
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load spc settings"})
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *QC) PutSPCSettings(c *gin.Context) {
	var body struct {
		Rules        []int  `json:"rules"`
		Alarms       *bool  `json:"alarms"`
		RaiseNC      bool   `json:"raise_nc"`
		ChangeReason string `json:"change_reason"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	alarms := true
	if body.Alarms != nil {
		alarms = *body.Alarms
	}
	if body.Rules == nil {
		body.Rules = []int{}
	}
	item, err := h.Store.PutSPCSettings(c.Request.Context(), store.SPCSettings{Parameter: c.Param("parameter"),
		Rules: body.Rules, Alarms: alarms, RaiseNC: body.RaiseNC, ChangeReason: body.ChangeReason}, actorOf(c))
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not save spc settings"})
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *QC) QualityDashboard(c *gin.Context) {
	days, _ := strconv.Atoi(c.DefaultQuery("days", "30"))
	out, err := h.Store.QualityDashboard(c.Request.Context(), days)
	if err != nil {
		log.Printf("quality-control: quality dashboard: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not build the quality dashboard"})
		return
	}
	c.JSON(http.StatusOK, out)
}
