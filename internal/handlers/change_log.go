package handlers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"iag-quality-control/backend/internal/pdf"
)

// ListChangeLog serves the before/after history of the quality record (017):
// GET /change-log?entity=incoming-inspections&id=IIN-26-0004.
func (h *QC) ListChangeLog(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	items, err := h.Store.ListChangeLogBetween(c.Request.Context(), c.Query("entity"), c.Query("id"),
		c.Query("actor"), c.Query("from"), c.Query("to"), limit)
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list change log"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

// ChangeLogPDF prints the filtered change log for an auditor: the same
// filters as ListChangeLog, up to 2000 entries.
func (h *QC) ChangeLogPDF(c *gin.Context) {
	const max = 2000
	items, err := h.Store.ListChangeLogBetween(c.Request.Context(), c.Query("entity"), c.Query("id"),
		c.Query("actor"), c.Query("from"), c.Query("to"), max+1)
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not read change log"})
		return
	}
	data := pdf.AuditTrailData{GeneratedBy: actorOf(c), GeneratedAt: time.Now()}
	for _, f := range []struct{ label, key string }{{"entity", "entity"}, {"record", "id"}, {"user", "actor"}, {"from", "from"}, {"to", "to"}} {
		if v := c.Query(f.key); v != "" {
			data.Filters = append(data.Filters, f.label+" = "+v)
		}
	}
	if len(items) > max {
		items, data.Truncated = items[:max], true
	}
	for _, e := range items {
		data.Entries = append(data.Entries, pdf.AuditEntry{When: e.ChangedAt, Entity: e.Entity, Record: e.BusinessID,
			Op: e.Op, Actor: e.Actor, Reason: e.Reason, Changes: e.Changes})
	}
	raw, err := pdf.RenderAuditTrail(data)
	writePDFResponse(c, "audit-trail-"+time.Now().UTC().Format("20060102-1504")+".pdf", raw, err)
}
