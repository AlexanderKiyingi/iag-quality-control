package handlers

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"iag-quality-control/backend/internal/domain"
	"iag-quality-control/backend/internal/store"
)

// SamplingPlan previews the ISO 2859-1 plan for a lot (024): the screen shows
// the sample size and accept/reject numbers before anything is counted.
func (h *QC) SamplingPlan(c *gin.Context) {
	lot, _ := strconv.Atoi(c.Query("lot_size"))
	aql, _ := strconv.ParseFloat(c.Query("aql"), 64)
	if lot == 0 && aql == 0 {
		c.JSON(http.StatusOK, gin.H{"levels": domain.InspectionLevels, "aqls": domain.AQLs})
		return
	}
	plan, err := domain.NewSamplingPlan(lot, c.Query("level"), aql)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, plan)
}

func (h *QC) ScheduleStabilityPulls(c *gin.Context) {
	var body struct {
		Timepoints []int `json:"timepoints"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	items, err := h.Store.SchedulePullPoints(c.Request.Context(), c.Param("id"), body.Timepoints)
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not schedule pulls"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

func (h *QC) ListStabilityPulls(c *gin.Context) {
	items, err := h.Store.ListPullPoints(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list pulls"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

func (h *QC) RecordStabilityResults(c *gin.Context) {
	day, err := strconv.Atoi(c.Param("day"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "the timepoint is a day number"})
		return
	}
	var body struct {
		Results []store.StabilityReading `json:"results"`
		Notes   string                   `json:"notes"`
	}
	if err := bindJSONCoerced(c, &body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	items, err := h.Store.RecordStabilityResults(c.Request.Context(), c.Param("id"), day, body.Results, body.Notes, actorOf(c))
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not record results"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

func (h *QC) StabilityShelfLife(c *gin.Context) {
	opt := func(k string) *float64 {
		if v, err := strconv.ParseFloat(c.Query(k), 64); err == nil {
			return &v
		}
		return nil
	}
	parameter := c.Query("parameter")
	est, specID, err := h.Store.StabilityShelfLife(c.Request.Context(), c.Param("id"), parameter, c.Query("grade"), opt("lsl"), opt("usl"))
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not estimate shelf life"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"study_business_id": c.Param("id"), "parameter": store.CanonicalParameter(parameter),
		"spec_id": specID, "estimate": est})
}

func (h *QC) DueStabilityPulls(c *gin.Context) {
	within, _ := strconv.Atoi(c.DefaultQuery("within", "14"))
	items, err := h.Store.DueStabilityPulls(c.Request.Context(), within, false)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list due pulls"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

/*
stabilityReminderLoop sends one qc.alert per stability pull, three days before
it is due (or as soon as it is found overdue), so a timepoint is not missed
because nobody opened the study. Each pull is reminded once.
*/
func (h *QC) stabilityReminderLoop(ctx context.Context, every time.Duration) {
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		due, err := h.Store.DueStabilityPulls(ctx, 3, true)
		if err != nil {
			log.Printf("quality-control: stability reminders: %v", err)
		}
		for _, d := range due {
			h.notifyAlert(ctx, "qc.alert", fmt.Sprintf("Stability pull due: %s day %d", d.StudyBusinessID, d.PullDay),
				fmt.Sprintf("The day-%d pull of stability study %s (%s) is due %s. Owner: %s.",
					d.PullDay, d.StudyBusinessID, d.Product, d.DueDate, d.Owner))
			if err := h.Store.MarkPullReminded(ctx, d.StudyBusinessID, d.PullDay); err != nil {
				log.Printf("quality-control: mark reminded: %v", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
