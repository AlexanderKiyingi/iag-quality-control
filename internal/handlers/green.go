package handlers

import (
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"iag-quality-control/backend/internal/domain"
	"iag-quality-control/backend/internal/store"
)

// GreenGradingCatalog serves what a grading sheet is built from: the SCA
// defect table with its categories and equivalents, and the screens.
func (h *QC) GreenGradingCatalog(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"defect_types":  domain.DefectTypes,
		"screens":       domain.Screens,
		"sample_grams":  domain.StandardSampleGrams,
		"grades":        []string{domain.GreenSpecialty, domain.GreenPremium, domain.GreenExchange, domain.GreenBelowStandard, domain.GreenOffGrade},
		"metric_params": []string{"full_defects", "category1_full_defects", "category2_full_defects", "quakers", "moisture", "water_activity", "density_g_l", "screen_<n>_plus_pct"},
	})
}

func (h *QC) ListGreenGradings(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "200"))
	items, err := h.Store.ListGreenGradings(c.Request.Context(), c.Query("sample_business_id"),
		c.Query("batch_business_id"), c.Query("include_void") == "true", limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list green gradings"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

func (h *QC) GetGreenGrading(c *gin.Context) {
	item, err := h.Store.GetGreenGrading(c.Request.Context(), c.Param("id"))
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load green grading"})
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *QC) CreateGreenGrading(c *gin.Context) {
	var body store.CreateGreenGradingInput
	if err := bindJSONCoerced(c, &body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	body.Actor = actorOf(c)
	item, err := h.Store.CreateGreenGrading(c.Request.Context(), body)
	if errors.Is(err, store.ErrAutoActions) {
		log.Printf("quality-control: green grading %s: %v", item.BusinessID, err)
		err = nil
	}
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not record green grading"})
		return
	}
	h.announceAutoActions(c.Request.Context(), item.AutoActions, item.BusinessID)
	params := make([]string, 0, len(item.Result.Metrics))
	for k := range item.Result.Metrics {
		params = append(params, k)
	}
	item.SPCAlerts = h.checkSPC(c.Request.Context(), item.BusinessID, actorOf(c), params...)
	c.JSON(http.StatusCreated, item)
}

// VoidGreenGrading withdraws a wrong grading; it needs a reason.
func (h *QC) VoidGreenGrading(c *gin.Context) {
	var body struct {
		Reason string `json:"reason"`
	}
	_ = c.ShouldBindJSON(&body)
	item, err := h.Store.VoidGreenGrading(c.Request.Context(), c.Param("id"), actorOf(c), body.Reason)
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not void green grading"})
		return
	}
	c.JSON(http.StatusOK, item)
}
