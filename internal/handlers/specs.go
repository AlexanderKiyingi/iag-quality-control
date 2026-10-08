package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"iag-quality-control/backend/internal/middleware"
	"iag-quality-control/backend/internal/store"
)

// Specification limits (migration 016). Reading them is open to anyone who can
// see the lab; changing them is its own permission, because a spec now decides
// what is automatically held and raised as a non-conformance.

func (h *QC) ListSpecifications(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "500"))
	inactive := c.Query("include_inactive") == "true"
	items, err := h.Store.ListSpecifications(c.Request.Context(), c.Query("stage"), c.Query("parameter"), inactive, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list specifications"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

func (h *QC) GetSpecification(c *gin.Context) {
	item, err := h.Store.GetSpecification(c.Request.Context(), c.Param("id"))
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load specification"})
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *QC) UpsertSpecification(c *gin.Context) {
	var body struct {
		BusinessID   string         `json:"business_id"`
		Parameter    string         `json:"parameter"`
		Stage        *string        `json:"stage"`
		Grade        *string        `json:"grade"`
		Label        *string        `json:"label"`
		Unit         *string        `json:"unit"`
		LSL          *string        `json:"lsl"`
		USL          *string        `json:"usl"`
		Target       *string        `json:"target"`
		ActionOnFail *string        `json:"action_on_fail"`
		Active       *bool          `json:"active"`
		Notes        *string        `json:"notes"`
		Attrs        map[string]any `json:"attrs"`
	}
	// Coerced: limits arrive as numbers from an API client and as strings from
	// a web form; the store wants strings so "" can mean "clear this limit".
	if err := bindJSONCoerced(c, &body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := h.Store.UpsertSpecification(c.Request.Context(), store.UpsertSpecificationInput{
		BusinessID: body.BusinessID, Parameter: body.Parameter, Stage: body.Stage, Grade: body.Grade,
		Label: body.Label, Unit: body.Unit, LSL: body.LSL, USL: body.USL, Target: body.Target,
		ActionOnFail: body.ActionOnFail, Active: body.Active, Notes: body.Notes, Attrs: body.Attrs,
		Actor: actorOf(c),
	})
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not save specification"})
		return
	}
	c.JSON(http.StatusOK, item)
}

/*
actorOf names who is making a request, for updated_by and the change log.

The verified token comes first. ActorLabel's X-User-Email header is a fallback
only: it is what the API audit has always recorded, but it is a header any
caller can set, and a change log is only worth having if its names are true.
*/
func actorOf(c *gin.Context) string {
	if claims, ok := middleware.PlatformClaims(c); ok && claims != nil {
		for _, v := range []string{claims.Email, claims.Name, claims.Subject, claims.ClientID} {
			if v = strings.TrimSpace(v); v != "" {
				return v
			}
		}
	}
	if label := middleware.ActorLabel(c); label != "anonymous" && label != "authenticated" {
		return label + " (unverified)"
	}
	return ""
}

/*
announceAutoActions tells the rest of the platform what a failing record set in
motion — once, when it was first raised, never on a re-save.

A new hold emits qc.batch.held, the same event a manual "hold" release decision
emits, so traceability's QR gate treats an automatic hold like a manual one.
Every new NC notifies the quality audience. Both are best effort: the hold and
the NC are already committed, and failing the user's save because Kafka is down
would invite them to save again, not to fix anything.
*/
func (h *QC) announceAutoActions(ctx context.Context, auto *store.AutoActions, sourceRef string) {
	if auto == nil || !auto.Created {
		return
	}
	if auto.HoldEventID != "" {
		_ = h.publish(ctx, "qc.batch.held", map[string]any{
			"batch_business_id":  auto.HoldRef,
			"hold_business_id":   auto.HoldEventID,
			"ncr_business_id":    auto.NonConformanceID,
			"source_business_id": sourceRef,
			"decision":           "hold",
			"decided_by":         "specification",
			"automatic":          true,
		})
	}
	what := "Non-conformance " + auto.NonConformanceID + " raised"
	if auto.HoldEventID != "" {
		what += fmt.Sprintf(" and %s placed on hold (%s)", auto.HoldRef, auto.HoldEventID)
	}
	h.notifyAlert(ctx, "qc.alert", "Out of specification: "+sourceRef,
		what+" because "+sourceRef+" failed its specification.")
}
