package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"iag-quality-control/backend/internal/store"
)

/*
SPCAnalytics charts one parameter: GET /analytics/spc?metric=caffeine.

Limits come from, in order: ?usl= / ?lsl= on the request (a what-if), else the
active specification for the parameter (016) at ?stage= and ?grade= (both
optional), else none — a chart with control limits and no capability index.
The moisture chart used to borrow an invented LSL of 10.3 here; the seeded spec
has no floor, so its Cpk is now the one-sided Cpu.
*/
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
	if opts.USL == nil && opts.LSL == nil {
		spec, err := h.Store.ResolveSpec(c.Request.Context(), c.Query("stage"), metric, c.Query("grade"))
		switch {
		case err == nil:
			opts.USL, opts.LSL, opts.SpecID = spec.USL, spec.LSL, spec.BusinessID
		case !errors.Is(err, store.ErrNotFound):
			c.JSON(http.StatusInternalServerError, gin.H{"error": "spc analytics failed"})
			return
		}
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

// SPCParameters lists the parameters that have data to chart.
func (h *QC) SPCParameters(c *gin.Context) {
	days, _ := strconv.Atoi(c.DefaultQuery("days", "30"))
	items, err := h.Store.SPCParameters(c.Request.Context(), days)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list spc parameters"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}
