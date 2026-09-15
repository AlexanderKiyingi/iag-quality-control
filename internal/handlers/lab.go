package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"iag-quality-control/backend/internal/store"
)

// Lab methods and requests (migration 010): upserted by business_id like
// instruments, so a form save is idempotent.

func (h *QC) ListLabMethods(c *gin.Context) {
	items, err := h.Store.ListLabMethods(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list lab methods"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

func (h *QC) UpsertLabMethod(c *gin.Context) {
	var body store.LabMethod
	if err := bindJSONCoerced(c, &body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := h.Store.UpsertLabMethod(c.Request.Context(), body)
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not save lab method"})
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *QC) ListLabRequests(c *gin.Context) {
	items, err := h.Store.ListLabRequests(c.Request.Context(), c.Query("status"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list lab requests"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

func (h *QC) UpsertLabRequest(c *gin.Context) {
	var body store.LabRequest
	if err := bindJSONCoerced(c, &body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := h.Store.UpsertLabRequest(c.Request.Context(), body)
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not save lab request"})
		return
	}
	c.JSON(http.StatusOK, item)
}
