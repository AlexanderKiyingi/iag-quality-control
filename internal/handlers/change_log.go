package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
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
