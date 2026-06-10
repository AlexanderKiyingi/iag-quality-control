package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"iag-quality-control/backend/internal/events"
	"iag-quality-control/backend/internal/store"
)

func (h *QC) PostCupping(c *gin.Context) {
	var body struct {
		Scorers    []string `json:"scorers"`
		Fragrance  float64  `json:"fragrance"`
		Flavor     float64  `json:"flavor"`
		Aftertaste float64  `json:"aftertaste"`
		Acidity    float64  `json:"acidity"`
		Body       float64  `json:"body"`
		Balance    float64  `json:"balance"`
		Uniformity float64  `json:"uniformity"`
		CleanCup   float64  `json:"cleancup"`
		Sweetness  float64  `json:"sweetness"`
		Overall    float64  `json:"overall"`
		DefectCat1 int      `json:"defect_cat1"`
		DefectCat2 int      `json:"defect_cat2"`
		Notes      string   `json:"notes"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	session, err := h.Store.CreateCupping(c.Request.Context(), store.CreateCuppingInput{
		SampleBusinessID: c.Param("id"),
		Scorers:          body.Scorers,
		Fragrance:        body.Fragrance,
		Flavor:           body.Flavor,
		Aftertaste:       body.Aftertaste,
		Acidity:          body.Acidity,
		Body:             body.Body,
		Balance:          body.Balance,
		Uniformity:       body.Uniformity,
		CleanCup:         body.CleanCup,
		Sweetness:        body.Sweetness,
		Overall:          body.Overall,
		DefectCat1:       body.DefectCat1,
		DefectCat2:       body.DefectCat2,
		Notes:            body.Notes,
	})
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not record cupping"})
		return
	}
	summary, err := h.Store.GetBatchLabSummary(c.Request.Context(), session.BatchBusinessID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load lab summary"})
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
	c.JSON(http.StatusCreated, gin.H{"session": session, "summary": summary})
}
