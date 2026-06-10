package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"iag-quality-control/backend/internal/events"
	"iag-quality-control/backend/internal/store"
)

func (h *QC) PostPhysicalTest(c *gin.Context) {
	var body struct {
		TechID        string   `json:"tech_id"`
		MoisturePct   *float64 `json:"moisture_pct"`
		Screen18      *float64 `json:"screen_18"`
		Screen17      *float64 `json:"screen_17"`
		Screen16      *float64 `json:"screen_16"`
		Screen15      *float64 `json:"screen_15"`
		ScreenPan     *float64 `json:"screen_pan"`
		BulkDensityGL *float64 `json:"bulk_density_g_l"`
		DefectCat1    int      `json:"defect_cat1"`
		DefectCat2    int      `json:"defect_cat2"`
		Status        string   `json:"status"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	test, err := h.Store.CreatePhysicalTest(c.Request.Context(), store.CreatePhysicalTestInput{
		SampleBusinessID: c.Param("id"),
		TechID:           body.TechID,
		MoisturePct:      body.MoisturePct,
		Screen18:         body.Screen18,
		Screen17:         body.Screen17,
		Screen16:         body.Screen16,
		Screen15:         body.Screen15,
		ScreenPan:        body.ScreenPan,
		BulkDensityGL:    body.BulkDensityGL,
		DefectCat1:       body.DefectCat1,
		DefectCat2:       body.DefectCat2,
		Status:           body.Status,
	})
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not record physical test"})
		return
	}

	var moisture *float64
	var defects *int
	if test.MoisturePct != nil {
		moisture = test.MoisturePct
	}
	if test.DefectCat2 > 0 {
		defects = &test.DefectCat2
	}
	summary, err := h.Store.UpsertBatchLabSummary(c.Request.Context(), store.UpsertLabSummaryInput{
		BatchBusinessID: test.BatchBusinessID,
		Moisture:        moisture,
		Defects:         defects,
		Tester:          test.TechID,
		LatestSampleID:  test.SampleBusinessID,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not update lab summary"})
		return
	}
	if err := h.publish(c.Request.Context(), "qc.lab.result_recorded", labResultPayload(summary)); err != nil {
		if errors.Is(err, events.ErrDisabled) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "kafka unavailable"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "kafka publish failed"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"test": test, "summary": summary})
}

func (h *QC) PostChemicalTest(c *gin.Context) {
	var body struct {
		TechID         string   `json:"tech_id"`
		MoistureKF      *float64 `json:"moisture_kf"`
		WaterActivity  *float64 `json:"water_activity"`
		PH             *float64 `json:"ph"`
		ChlorogenicPct *float64 `json:"chlorogenic_pct"`
		CaffeinePct    *float64 `json:"caffeine_pct"`
		Status         string   `json:"status"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	test, err := h.Store.CreateChemicalTest(c.Request.Context(), store.CreateChemicalTestInput{
		SampleBusinessID: c.Param("id"),
		TechID:           body.TechID,
		MoistureKF:       body.MoistureKF,
		WaterActivity:    body.WaterActivity,
		PH:               body.PH,
		ChlorogenicPct:   body.ChlorogenicPct,
		CaffeinePct:      body.CaffeinePct,
		Status:           body.Status,
	})
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not record chemical test"})
		return
	}

	summary, err := h.Store.UpsertBatchLabSummary(c.Request.Context(), store.UpsertLabSummaryInput{
		BatchBusinessID: test.BatchBusinessID,
		Moisture:        test.MoistureKF,
		WaterActivity:   test.WaterActivity,
		Tester:          test.TechID,
		LatestSampleID:  test.SampleBusinessID,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not update lab summary"})
		return
	}
	if err := h.publish(c.Request.Context(), "qc.lab.result_recorded", labResultPayload(summary)); err != nil {
		if errors.Is(err, events.ErrDisabled) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "kafka unavailable"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "kafka publish failed"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"test": test, "summary": summary})
}
