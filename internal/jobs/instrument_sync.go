package jobs

import (
	"context"
	"log"
	"strings"
	"time"

	"iag-quality-control/backend/internal/clients"
	"iag-quality-control/backend/internal/store"
)

func SyncInstrumentsFromMES(ctx context.Context, st *store.Store, mes *clients.MES) (int, error) {
	if mes == nil || !mes.Enabled() {
		return 0, nil
	}
	items, err := st.ListInstrumentsForSync(ctx)
	if err != nil {
		return 0, err
	}
	updated := 0
	for _, ins := range items {
		tag := strings.TrimSpace(ins.MESAssetTag)
		if tag == "" {
			continue
		}
		points, err := mes.ListAssetTelemetry(ctx, tag)
		if err != nil {
			log.Printf("quality-control: instrument sync %s: %v", ins.BusinessID, err)
			continue
		}
		if len(points) == 0 {
			continue
		}
		var samples24h int
		var status string
		var lastVal float64
		var lastAt time.Time
		for _, p := range points {
			if p.RecordedAt.After(lastAt) {
				lastAt = p.RecordedAt
				lastVal = p.Value
			}
			metric := strings.ToLower(p.Metric)
			switch metric {
			case "samples_24h", "sample_count", "samples":
				samples24h = int(p.Value)
			case "status", "online":
				if p.Value >= 1 {
					status = "online"
				} else {
					status = "offline"
				}
			}
		}
		if err := st.ApplyInstrumentTelemetry(ctx, ins.BusinessID, store.InstrumentTelemetryUpdate{
			Samples24h:       samples24h,
			Status:           status,
			LastReadingAt:    &lastAt,
			LastReadingValue: &lastVal,
		}); err != nil {
			log.Printf("quality-control: instrument update %s: %v", ins.BusinessID, err)
			continue
		}
		updated++
	}
	return updated, nil
}

func StartInstrumentSyncLoop(ctx context.Context, st *store.Store, mes *clients.MES, interval time.Duration) {
	if mes == nil || !mes.Enabled() || interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	run := func() {
		n, err := SyncInstrumentsFromMES(ctx, st, mes)
		if err != nil {
			log.Printf("quality-control: instrument sync failed: %v", err)
			return
		}
		if n > 0 {
			log.Printf("quality-control: instrument sync updated %d instruments", n)
		}
	}
	run()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}
