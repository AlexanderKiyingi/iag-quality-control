package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"iag-quality-control/backend/internal/events"
	"iag-quality-control/backend/internal/store"
)

func (h *QC) PostCupping(c *gin.Context) {
	var body struct {
		Scorers    []string       `json:"scorers"`
		Fragrance  float64        `json:"fragrance"`
		Flavor     float64        `json:"flavor"`
		Aftertaste float64        `json:"aftertaste"`
		Acidity    float64        `json:"acidity"`
		Body       float64        `json:"body"`
		Balance    float64        `json:"balance"`
		Uniformity float64        `json:"uniformity"`
		CleanCup   float64        `json:"cleancup"`
		Sweetness  float64        `json:"sweetness"`
		Overall    float64        `json:"overall"`
		DefectCat1 int            `json:"defect_cat1"`
		DefectCat2 int            `json:"defect_cat2"`
		Notes      string         `json:"notes"`
		Attrs      map[string]any `json:"attrs"`
		// 018: one sheet per evaluator. When sent, the scores above are
		// ignored and the session stores the panel mean.
		Scores []store.CuppingScoreInput `json:"scores"`
	}
	if err := bindJSONCoerced(c, &body); err != nil {
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
		Attrs:            body.Attrs,
		Scores:           body.Scores,
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
	// A failing/low cupping grade ("Reject", i.e. SCA score < 75) is a genuine
	// quality failure worth notifying the QC manager about.
	if strings.EqualFold(summary.Grade, "Reject") {
		score := "n/a"
		if summary.CupScore != nil {
			score = strconv.FormatFloat(*summary.CupScore, 'f', -1, 64)
		}
		h.notifyAlert(c.Request.Context(), "qc.alert",
			"Cupping failed for batch "+summary.BatchBusinessID,
			"Batch "+summary.BatchBusinessID+" graded "+summary.Grade+" (SCA score "+score+").")
	}
	resp := gin.H{"session": session, "summary": summary}
	if len(body.Scores) > 0 {
		panel, err := h.Store.GetCuppingPanel(c.Request.Context(), session.BusinessID, 0)
		if err == nil {
			resp["panel"] = panel
			// A panel that does not agree is worth a look before the score is
			// relied on: an uncalibrated cupper, or a cup that was not the
			// coffee on the label.
			if panel.Stats.OutlierCount > 0 {
				h.notifyAlert(c.Request.Context(), "qc.alert",
					"Cupping panel disagreement on "+session.BusinessID,
					strconv.Itoa(panel.Stats.OutlierCount)+" of "+strconv.Itoa(panel.Stats.PanelSize)+
						" evaluators scored more than "+strconv.FormatFloat(panel.Stats.OutlierThreshold, 'f', -1, 64)+
						" points from the panel median on batch "+session.BatchBusinessID+".")
			}
		}
	}
	c.JSON(http.StatusCreated, resp)
}

// SaveCuppingScore records one evaluator's sheet on an existing session (018)
// and returns it; the session's mean is recomputed.
func (h *QC) SaveCuppingScore(c *gin.Context) {
	var body store.CuppingScoreInput
	if err := bindJSONCoerced(c, &body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := h.Store.SaveCuppingScore(c.Request.Context(), c.Param("id"), body)
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not save cupping sheet"})
		return
	}
	c.JSON(http.StatusOK, item)
}

// ListCuppingScores lists sheets with each one's deviation in its panel.
func (h *QC) ListCuppingScores(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "500"))
	items, err := h.Store.ListCuppingScores(c.Request.Context(), c.Query("session"), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list cupping sheets"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

// GetCuppingPanel serves a session's per-evaluator sheets and the panel's
// agreement statistics (018). ?threshold= overrides the outlier distance.
func (h *QC) GetCuppingPanel(c *gin.Context) {
	threshold, _ := strconv.ParseFloat(c.Query("threshold"), 64)
	panel, err := h.Store.GetCuppingPanel(c.Request.Context(), c.Param("id"), threshold)
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load cupping panel"})
		return
	}
	c.JSON(http.StatusOK, panel)
}
