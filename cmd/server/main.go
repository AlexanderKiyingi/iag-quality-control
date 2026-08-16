package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/alvor-technologies/iag-platform-go/authclient"
	platformotel "github.com/alvor-technologies/iag-platform-go/otel"

	"iag-quality-control/backend/internal/auditlog"
	"iag-quality-control/backend/internal/clients"
	"iag-quality-control/backend/internal/config"
	"iag-quality-control/backend/internal/db"
	"iag-quality-control/backend/internal/events"
	"iag-quality-control/backend/internal/handlers"
	"iag-quality-control/backend/internal/jobs"
	"iag-quality-control/backend/internal/middleware"
	"iag-quality-control/backend/internal/migrate"
	"iag-quality-control/backend/internal/outbox"
	"iag-quality-control/backend/internal/store"
)

func main() {
	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	// OpenTelemetry → otel-collector:4317 (non-blocking dial).
	if tp, err := platformotel.Init(ctx, platformotel.Config{
		ServiceName: cfg.ServiceName,
		Environment: cfg.Environment,
	}); err != nil {
		log.Printf("otel disabled: %v", err)
	} else {
		defer func() {
			sc, c := context.WithTimeout(context.Background(), 5*time.Second)
			defer c()
			_ = tp.Shutdown(sc)
		}()
	}

	pool, err := db.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer pool.Close()

	if cfg.AutoMigrate {
		if err := migrate.Up(ctx, pool); err != nil {
			log.Fatalf("migrate: %v", err)
		}
	}

	pub := events.NewPublisher(cfg.KafkaBrokers, cfg.KafkaTopic, cfg.KafkaClientID)
	defer pub.Close()

	// Durable event outbox: handlers enqueue, the relay drains to Kafka with
	// retry so CoA/lab-result events survive a transient broker outage.
	var outboxStore *outbox.Store
	if pub.Enabled() {
		outboxStore = outbox.NewStore(pool)
		relay := outbox.NewPublisher(outboxStore, outboxDispatcher{pub: pub})
		go relay.Run(ctx)
	}

	scm := clients.NewSCM(cfg.UpstreamSCM, cfg.AuthTokenURL, cfg.ServiceClientID, cfg.ServiceClientSecret)
	mes := clients.NewMES(cfg.UpstreamMES, cfg.AuthTokenURL, cfg.ServiceClientID, cfg.ServiceClientSecret)

	var verifier *authclient.Verifier
	if cfg.AuthMode == "jwt" {
		verifier = authclient.NewVerifier(authclient.Options{
			JWKSURL:  cfg.JWKSURL,
			Issuer:   cfg.JWTIssuer,
			Audience: cfg.Audience,
		})
		// Tolerate transient JWKS failure on boot. A hard exit here turns an
		// auth-service redeploy into a crash loop for this service, and the
		// container never gets far enough to serve /health — which reads as
		// "quality-control is down" rather than "auth was briefly unavailable".
		bootstrapJWKS(verifier)
		go jwksRefreshLoop(ctx, verifier)
	}

	platformAuth := middleware.NewPlatformAuth(middleware.PlatformAuthOptions{
		Mode:     cfg.AuthMode,
		Verifier: verifier,
	})

	go registerPermissionsLoop(ctx, cfg)
	if mes.Enabled() {
		go jobs.StartInstrumentSyncLoop(ctx, store.New(pool), mes, cfg.InstrumentSyncInterval)
	}

	auditStore := auditlog.NewStore(pool)
	router := handlers.NewRouter(handlers.RouterDeps{
		Cfg:          cfg,
		Pool:         pool,
		Pub:          pub,
		Outbox:       outboxStore,
		Audit:        auditStore,
		SCM:          scm,
		MES:          mes,
		PlatformAuth: platformAuth,
		StrictRBAC:   cfg.StrictRBAC(),
	})

	srv := &http.Server{Addr: ":" + cfg.Port, Handler: router, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		log.Printf("quality-control listening on :%s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}

// outboxDispatcher adapts the Kafka events.Publisher to the outbox.Dispatcher
// interface. It reconstructs the event from the stored payload and lets the
// Publisher build the canonical envelope + Kafka key, preserving the exact wire
// contract consumed by traceability.
type outboxDispatcher struct {
	pub *events.Publisher
}

func (d outboxDispatcher) DispatchOutbox(ctx context.Context, row outbox.Row) error {
	var data map[string]any
	if err := json.Unmarshal(row.Payload, &data); err != nil {
		return err
	}
	return d.pub.Publish(ctx, row.EventType, data)
}

// bootstrapJWKS performs the initial JWKS fetch with exponential backoff so a
// transient failure (auth cold start, redeploy, network blip) does not kill the
// container. Budget ~2 minutes; returns once keys are loaded or the budget is
// spent, and the caller starts the refresh loop either way.
func bootstrapJWKS(v *authclient.Verifier) {
	backoff := time.Second
	const (
		maxBackoff   = 15 * time.Second
		totalBudget  = 2 * time.Minute
		perAttemptTO = 10 * time.Second
	)
	deadline := time.Now().Add(totalBudget)
	for attempt := 1; ; attempt++ {
		attemptCtx, cancel := context.WithTimeout(context.Background(), perAttemptTO)
		err := v.Refresh(attemptCtx)
		cancel()
		if err == nil {
			log.Printf("jwks bootstrap ok (attempt %d)", attempt)
			return
		}
		if time.Now().After(deadline) {
			log.Printf("jwks bootstrap budget exhausted after %d attempts; continuing with empty key set: %v", attempt, err)
			return
		}
		log.Printf("jwks bootstrap failed (attempt %d), retrying in %s: %v", attempt, backoff, err)
		time.Sleep(backoff)
		if backoff < maxBackoff {
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
}

// jwksRefreshLoop keeps the key set current at two speeds: the steady rotation
// interval once keys are loaded, and a much shorter one while the set is empty.
// Empty is not a mild degradation — every authenticated request fails closed —
// so recovery has to be seconds, not a quarter of an hour.
func jwksRefreshLoop(ctx context.Context, v *authclient.Verifier) {
	const (
		steadyInterval   = 15 * time.Minute
		degradedInterval = 15 * time.Second
	)
	for {
		wait := steadyInterval
		if !v.HasKeys() {
			wait = degradedInterval
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}

		hadKeys := v.HasKeys()
		refreshCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		err := v.Refresh(refreshCtx)
		cancel()
		switch {
		case err != nil && hadKeys:
			// Previous key set is still in memory; tokens keep verifying.
			log.Printf("jwks refresh failed; serving with the previous key set: %v", err)
		case err != nil:
			log.Printf("jwks still unavailable; all authenticated requests are being rejected: %v", err)
		case !hadKeys:
			log.Printf("jwks recovered; token verification restored")
		}
	}
}
