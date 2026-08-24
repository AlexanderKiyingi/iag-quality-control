package handlers

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"

	"iag-quality-control/backend/internal/clients"
	"iag-quality-control/backend/internal/config"
	"iag-quality-control/backend/internal/events"
	"iag-quality-control/backend/internal/outbox"
	"iag-quality-control/backend/internal/store"
)

type QC struct {
	Cfg    config.Config
	Store  *store.Store
	Pub    *events.Publisher
	Outbox *outbox.Store
	SCM    *clients.SCM
	MES    *clients.MES
}

// publish emits a domain event. When Kafka is enabled it durably enqueues to
// the outbox (the relay drains it to Kafka with retry); when Kafka is disabled
// (e.g. local dev) it falls through to the no-op publisher.
func (h *QC) publish(ctx context.Context, eventType string, data map[string]any) error {
	if h.Cfg.KafkaRequired && !h.Pub.Enabled() {
		return events.ErrDisabled
	}
	if h.Outbox != nil && h.Pub.Enabled() {
		return h.Outbox.Enqueue(ctx, eventType, data)
	}
	return h.Pub.Publish(ctx, eventType, data)
}

// notifyAlert emits qc.alert.raised (consumed by the central notifications
// service off the iag.quality topic) using the shared
// {channel,recipient,audience,templateId,variables} envelope. It is a
// best-effort, non-blocking side channel that never fails the domain request.
//
// It addresses the "approvals.quality" audience, whose recipients an
// administrator maintains centrally; NOTIFY_DEFAULT_RECIPIENT is the fallback
// used until that audience is routed. Because the audience always travels, an
// unset recipient is no longer a silent drop — notifications records it as an
// unrouted audience and the admin UI lists it.
func (h *QC) notifyAlert(ctx context.Context, templateID, title, body string) {
	recipient := strings.TrimSpace(os.Getenv("NOTIFY_DEFAULT_RECIPIENT"))
	channel := strings.TrimSpace(os.Getenv("NOTIFY_CHANNEL"))
	if channel == "" {
		channel = "email"
	}
	_ = h.publish(ctx, "qc.alert.raised", map[string]any{
		"channel":    channel,
		"recipient":  recipient,
		"audience":   "approvals.quality",
		"templateId": templateID,
		"variables":  map[string]any{"Title": title, "Body": body},
	})
}

func (h *QC) validateBatch(ctx context.Context, batchID string) error {
	if !h.Cfg.AutoValidateBatchSCM || h.SCM == nil || !h.SCM.Enabled() {
		return nil
	}
	ok, err := h.SCM.ValidateBatch(ctx, batchID)
	if err != nil {
		return err
	}
	if !ok {
		return store.ErrNotFound
	}
	return nil
}

func (h *QC) validateExportLot(ctx context.Context, lotID string) error {
	if lotID == "" || !h.Cfg.AutoValidateExportLotSCM || h.SCM == nil || !h.SCM.Enabled() {
		return nil
	}
	ok, err := h.SCM.ValidateExportLot(ctx, lotID)
	if err != nil {
		return err
	}
	if !ok {
		return store.ErrNotFound
	}
	return nil
}

func respondStoreErr(c *gin.Context, err error) bool {
	switch {
	case errors.Is(err, store.ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return true
	case errors.Is(err, store.ErrConflict):
		c.JSON(http.StatusConflict, gin.H{"error": "conflict"})
		return true
	case errors.Is(err, store.ErrBadInput):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return true
	default:
		return false
	}
}

func labResultPayload(summary store.BatchLabSummary) map[string]any {
	data := map[string]any{"batch_business_id": summary.BatchBusinessID}
	if summary.Moisture != nil {
		data["moisture"] = *summary.Moisture
	}
	if summary.CupScore != nil {
		data["cup_score"] = *summary.CupScore
	}
	if summary.WaterActivity != nil {
		data["water_activity"] = *summary.WaterActivity
	}
	if summary.Grade != "" {
		data["grade"] = summary.Grade
	}
	if summary.Defects != nil {
		data["defects"] = *summary.Defects
	}
	if summary.Tester != "" {
		data["tester"] = summary.Tester
	}
	if summary.LatestSampleID != "" {
		data["sample_id"] = summary.LatestSampleID
	}
	return data
}

