package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"iag-quality-control/backend/internal/store"
)

func (h *QC) SPCAnalytics(c *gin.Context) {
	metric := c.DefaultQuery("metric", "moisture")
	days, _ := strconv.Atoi(c.DefaultQuery("days", "30"))
	opts := store.SPCOptions{Metric: metric, BatchID: c.Query("batch_business_id"), Days: days}
	if v := c.Query("usl"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			opts.USL = &f
		}
	}
	if v := c.Query("lsl"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			opts.LSL = &f
		}
	}
	if opts.USL == nil && metric == "moisture" {
		usl, lsl := 12.5, 10.3
		opts.USL, opts.LSL = &usl, &lsl
	}
	series, err := h.Store.SPC(c.Request.Context(), opts)
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "spc analytics failed"})
		return
	}
	c.JSON(http.StatusOK, series)
}
