package handlers

import (
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"iag-quality-control/backend/internal/events"
	"iag-quality-control/backend/internal/store"
)

// Non-conformances, in-process checks, release decisions and hold events
// (migration 012). Each is its own top-level collection; see router.go for why
// none of them is nested.

func (h *QC) ListNonConformances(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "200"))
	items, err := h.Store.ListNonConformances(c.Request.Context(), c.Query("status"), c.Query("severity"), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list non-conformances"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

func (h *QC) GetNonConformance(c *gin.Context) {
	item, err := h.Store.GetNonConformance(c.Request.Context(), c.Param("id"))
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not get non-conformance"})
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *QC) UpsertNonConformance(c *gin.Context) {
	var body struct {
		BusinessID  string         `json:"business_id"`
		Title       string         `json:"title"`
		RaisedDate  *string        `json:"raised_date"`
		SourceRef   *string        `json:"source_ref"`
		Severity    *string        `json:"severity"`
		Owner       *string        `json:"owner"`
		Status      *string        `json:"status"`
		Description *string        `json:"description"`
		RootCause   *string        `json:"root_cause"`
		Attachments *string        `json:"attachments"`
		Attrs       map[string]any `json:"attrs"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := h.Store.UpsertNonConformance(c.Request.Context(), store.UpsertNonConformanceInput{
		BusinessID: body.BusinessID, Title: body.Title, RaisedDate: body.RaisedDate,
		SourceRef: body.SourceRef, Severity: body.Severity, Owner: body.Owner,
		Status: body.Status, Description: body.Description, RootCause: body.RootCause,
		Attachments: body.Attachments, Attrs: body.Attrs, Actor: actorOf(c),
	})
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not save non-conformance"})
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *QC) ListInProcessChecks(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "200"))
	items, err := h.Store.ListInProcessChecks(c.Request.Context(), c.Query("batch"), c.Query("status"), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list in-process checks"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

func (h *QC) UpsertInProcessCheck(c *gin.Context) {
	var body struct {
		BusinessID string         `json:"business_id"`
		Parameter  string         `json:"parameter"`
		CheckDate  *string        `json:"check_date"`
		Stage      *string        `json:"stage"`
		BatchRef   *string        `json:"batch_ref"`
		Target     *string        `json:"target"`
		Actual     *string        `json:"actual"`
		CheckedBy  *string        `json:"checked_by"`
		Result     *string        `json:"result"`
		Status     *string        `json:"status"`
		Notes      *string        `json:"notes"`
		Attrs      map[string]any `json:"attrs"`
		// 016: why the checker's result disagrees with the verdict.
		OverrideReason *string `json:"override_reason"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := h.Store.UpsertInProcessCheck(c.Request.Context(), store.UpsertInProcessCheckInput{
		BusinessID: body.BusinessID, Parameter: body.Parameter, CheckDate: body.CheckDate,
		Stage: body.Stage, BatchRef: body.BatchRef, Target: body.Target, Actual: body.Actual,
		CheckedBy: body.CheckedBy, Result: body.Result, Status: body.Status,
		Notes: body.Notes, Attrs: body.Attrs, OverrideReason: body.OverrideReason, Actor: actorOf(c),
	})
	if errors.Is(err, store.ErrAutoActions) {
		log.Printf("quality-control: in-process check %s: %v", item.BusinessID, err)
		err = nil
	}
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not save in-process check"})
		return
	}
	h.announceAutoActions(c.Request.Context(), item.AutoActions, item.BusinessID)
	c.JSON(http.StatusOK, item)
}

func (h *QC) ListReleaseDecisions(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "200"))
	items, err := h.Store.ListReleaseDecisions(c.Request.Context(), c.Query("batch"), c.Query("decision"), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list release decisions"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

/*
UpsertReleaseDecision saves the decision and, when it is new or changed,
announces it.

This is the only resource in the QA module that emits. A batch being released or
held is the same class of gate iag-traceability and iag-warehouse already key off
`qc.coa.issued`, so it is worth announcing; the other registers are lab-internal
and follow migration 010's precedent of emitting nothing.

The `emit` flag comes from the store, which compares against the previous
decision in the same statement as the write. Without it, editing the notes on an
already-released batch would re-announce the release and re-trigger real work
downstream.

The known wart, shared with every other publishing handler here: the row is
already committed when the publish runs, so a publish failure returns 5xx for a
write that did land. The row is echoed alongside the error so a client can
reconcile rather than blindly retry — a retry would create a second decision.
Fixing it properly means enqueuing the outbox row inside the domain transaction,
which is a refactor across five handlers and does not belong in this change.
*/
func (h *QC) UpsertReleaseDecision(c *gin.Context) {
	var body struct {
		BusinessID   string         `json:"business_id"`
		BatchRef     string         `json:"batch_ref"`
		DecisionDate *string        `json:"decision_date"`
		Product      *string        `json:"product"`
		CheckRefs    *string        `json:"check_refs"`
		DecidedBy    *string        `json:"decided_by"`
		Decision     *string        `json:"decision"`
		Status       *string        `json:"status"`
		Notes        *string        `json:"notes"`
		Attachments  *string        `json:"attachments"`
		Attrs        map[string]any `json:"attrs"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, emit, err := h.Store.UpsertReleaseDecision(c.Request.Context(), store.UpsertReleaseDecisionInput{
		BusinessID: body.BusinessID, BatchRef: body.BatchRef, DecisionDate: body.DecisionDate,
		Product: body.Product, CheckRefs: body.CheckRefs, DecidedBy: body.DecidedBy,
		Decision: body.Decision, Status: body.Status, Notes: body.Notes,
		Attachments: body.Attachments, Attrs: body.Attrs, Actor: actorOf(c),
	})
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not save release decision"})
		return
	}
	resp := gin.H{"release": item}
	if emit {
		if eventType := store.ReleaseEventType(item.Decision); eventType != "" {
			data := map[string]any{
				// batch_business_id is the key the publisher partitions on and the
				// key traceability resolves an entity from — the name matters.
				"batch_business_id":   item.BatchRef,
				"release_business_id": item.BusinessID,
				"product":             item.Product,
				"decision":            item.Decision,
				"decided_by":          item.DecidedBy,
				"check_refs":          item.CheckRefs,
			}
			if item.DecisionDate != nil {
				data["decision_date"] = *item.DecisionDate
			}
			if err := h.publish(c.Request.Context(), eventType, data); err != nil {
				if errors.Is(err, events.ErrDisabled) {
					c.JSON(http.StatusServiceUnavailable, gin.H{"error": "kafka unavailable", "release": item})
					return
				}
				c.JSON(http.StatusInternalServerError, gin.H{"error": "kafka publish failed", "release": item})
				return
			}
			resp["event_type"] = eventType
		}
	}
	c.JSON(http.StatusOK, resp)
}

func (h *QC) ListHoldEvents(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "200"))
	items, err := h.Store.ListHoldEvents(c.Request.Context(), c.Query("batch"), c.Query("status"), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list hold events"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

// CreateHoldEvent appends to the log. A repeated reference is a 409, not an
// update — an audit log that can be rewritten is not one.
func (h *QC) CreateHoldEvent(c *gin.Context) {
	var body struct {
		BusinessID   string         `json:"business_id"`
		BatchRef     string         `json:"batch_ref"`
		Event        string         `json:"event"`
		EventDate    string         `json:"event_date"`
		Reason       string         `json:"reason"`
		RecordedBy   string         `json:"recorded_by"`
		HoldLocation string         `json:"hold_location"`
		Status       string         `json:"status"`
		Notes        string         `json:"notes"`
		Attrs        map[string]any `json:"attrs"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := h.Store.CreateHoldEvent(c.Request.Context(), store.CreateHoldEventInput{
		BusinessID: body.BusinessID, BatchRef: body.BatchRef, Event: body.Event,
		EventDate: body.EventDate, Reason: body.Reason, RecordedBy: body.RecordedBy,
		HoldLocation: body.HoldLocation, Status: body.Status, Notes: body.Notes, Attrs: body.Attrs,
	})
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not record hold event"})
		return
	}
	c.JSON(http.StatusOK, item)
}
