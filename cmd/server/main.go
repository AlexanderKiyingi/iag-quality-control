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
		initCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		if err := verifier.Refresh(initCtx); err != nil {
			cancel()
			log.Fatalf("jwks refresh: %v", err)
		}
		cancel()
		go jwksRefreshLoop(verifier)
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

func jwksRefreshLoop(v *authclient.Verifier) {
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := v.Refresh(ctx); err != nil {
			log.Printf("jwks refresh: %v", err)
		}
		cancel()
	}
}
