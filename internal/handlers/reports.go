package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

func (h *QC) DayReport(c *gin.Context) {
	report, err := h.Store.DayReport(c.Request.Context(), c.Query("date"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "report failed"})
		return
	}
	c.JSON(http.StatusOK, report)
}

func (h *QC) TrendsReport(c *gin.Context) {
	days, _ := strconv.Atoi(c.DefaultQuery("days", "7"))
	points, err := h.Store.Trends(c.Request.Context(), days)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "trends failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"days": days, "points": points})
}

func (h *QC) WeeklySummary(c *gin.Context) {
	summary, err := h.Store.WeeklySummary(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "weekly summary failed"})
		return
	}
	c.JSON(http.StatusOK, summary)
}

func (h *QC) ExportReport(c *gin.Context) {
	exportType := strings.ToLower(c.DefaultQuery("type", "samples"))
	switch exportType {
	case "samples":
		items, err := h.Store.ListSamples(c.Request.Context(), c.Query("status"), c.Query("batch_business_id"), 500)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "export failed"})
			return
		}
		var b strings.Builder
		b.WriteString("business_id,batch_business_id,sample_type,status,priority,assigned_tech,received_at\n")
		for _, s := range items {
			b.WriteString(fmt.Sprintf("%s,%s,%s,%s,%s,%s,%s\n",
				csvEscape(s.BusinessID), csvEscape(s.BatchBusinessID), csvEscape(s.SampleType),
				csvEscape(s.Status), csvEscape(s.Priority), csvEscape(s.AssignedTech),
				s.ReceivedAt.Format("2006-01-02T15:04:05Z")))
		}
		c.Header("Content-Type", "text/csv; charset=utf-8")
		c.Header("Content-Disposition", `attachment; filename="qc-samples.csv"`)
		c.String(http.StatusOK, b.String())
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported export type", "supported": []string{"samples"}})
	}
}

func csvEscape(s string) string {
	if strings.ContainsAny(s, ",\"\n") {
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return s
}

func (h *QC) GetCoAByNumber(c *gin.Context) {
	coa, err := h.Store.GetCoAByNumber(c.Request.Context(), c.Param("coaNumber"))
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load coa"})
		return
	}
	c.JSON(http.StatusOK, coa)
}
