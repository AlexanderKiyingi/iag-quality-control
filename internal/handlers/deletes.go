package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"iag-quality-control/backend/internal/store"
)

// Guarded deletes (migration 015 work). See internal/store/deletes.go for why
// the list is this short: most of what this service holds is a quality record,
// and only planning data that has not been acted on may be removed.

func (h *QC) deleteFrom(table string) gin.HandlerFunc {
	return func(c *gin.Context) {
		err := h.Store.DeleteRegisterRow(c.Request.Context(), table, c.Param("id"))
		if err == store.ErrConflict {
			// Deletable in principle, but this row has moved on. Say what to do
			// instead rather than just refusing.
			c.JSON(http.StatusConflict, gin.H{
				"error": "this record has been acted on and is part of the history now — " +
					"set its status to void or cancelled instead of deleting it",
			})
			return
		}
		if respondStoreErr(c, err) {
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not delete"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"deleted": c.Param("id")})
	}
}
