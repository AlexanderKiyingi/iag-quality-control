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
	// Bound separately from store.LabMethod so the optional fields can be
	// pointers without turning every empty string in the response into null.
	var body struct {
		BusinessID         string         `json:"business_id"`
		Name               string         `json:"name"`
		Version            *string        `json:"version"`
		Scope              *string        `json:"scope"`
		Equipment          *string        `json:"equipment"`
		Procedure          *string        `json:"procedure"`
		AcceptanceCriteria *string        `json:"acceptance_criteria"`
		Status             *string        `json:"status"`
		Attrs              map[string]any `json:"attrs"`
	}
	if err := bindJSONCoerced(c, &body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := h.Store.UpsertLabMethod(c.Request.Context(), store.UpsertLabMethodInput{
		BusinessID:         body.BusinessID,
		Name:               body.Name,
		Version:            body.Version,
		Scope:              body.Scope,
		Equipment:          body.Equipment,
		Procedure:          body.Procedure,
		AcceptanceCriteria: body.AcceptanceCriteria,
		Status:             body.Status,
		Attrs:              body.Attrs,
	})
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
	var body struct {
		BusinessID  string         `json:"business_id"`
		Product     string         `json:"product"`
		RequestDate *string        `json:"request_date"`
		RequestedBy *string        `json:"requested_by"`
		Priority    *string        `json:"priority"`
		Objective   *string        `json:"objective"`
		NeededBy    *string        `json:"needed_by"`
		MethodID    *string        `json:"method_id"`
		Status      *string        `json:"status"`
		Notes       *string        `json:"notes"`
		Attrs       map[string]any `json:"attrs"`
	}
	if err := bindJSONCoerced(c, &body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := h.Store.UpsertLabRequest(c.Request.Context(), store.UpsertLabRequestInput{
		BusinessID:  body.BusinessID,
		Product:     body.Product,
		RequestDate: body.RequestDate,
		RequestedBy: body.RequestedBy,
		Priority:    body.Priority,
		Objective:   body.Objective,
		NeededBy:    body.NeededBy,
		MethodID:    body.MethodID,
		Status:      body.Status,
		Notes:       body.Notes,
		Attrs:       body.Attrs,
	})
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not save lab request"})
		return
	}
	c.JSON(http.StatusOK, item)
}
