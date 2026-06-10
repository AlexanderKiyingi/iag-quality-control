package handlers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"iag-quality-control/backend/internal/labels"
)

func (h *QC) SampleLabel(c *gin.Context) {
	sample, err := h.Store.GetSample(c.Request.Context(), c.Param("id"))
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load sample"})
		return
	}
	label := labels.NewSampleLabel(sample.BusinessID, sample.BatchBusinessID, sample.SampleType)
	format := strings.ToLower(c.DefaultQuery("format", "json"))
	switch format {
	case "zpl":
		c.Header("Content-Type", "text/plain; charset=utf-8")
		c.String(http.StatusOK, labels.RenderZPL(label))
	case "svg":
		c.Header("Content-Type", "image/svg+xml; charset=utf-8")
		c.String(http.StatusOK, labels.RenderSVG(label))
	default:
		c.JSON(http.StatusOK, label)
	}
}
