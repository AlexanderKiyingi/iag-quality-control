package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func (h *QC) Calendar(c *gin.Context) {
	events, err := h.Store.Calendar(c.Request.Context(), c.Query("from"), c.Query("to"))
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "calendar failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": events, "total": len(events)})
}
