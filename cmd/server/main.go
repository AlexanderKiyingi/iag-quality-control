package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"iag-quality-control/backend/internal/auditlog"
	"iag-quality-control/backend/internal/clients"
	"iag-quality-control/backend/internal/config"
	"iag-quality-control/backend/internal/db"
	"iag-quality-control/backend/internal/events"
	"iag-quality-control/backend/internal/handlers"
	"iag-quality-control/backend/internal/jobs"
	"iag-quality-control/backend/internal/migrate"
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

	scm := clients.NewSCM(cfg.UpstreamSCM, cfg.AuthTokenURL, cfg.ServiceClientID, cfg.ServiceClientSecret)
	mes := clients.NewMES(cfg.UpstreamMES, cfg.AuthTokenURL, cfg.ServiceClientID, cfg.ServiceClientSecret)

	go registerPermissionsLoop(ctx, cfg)
	if mes.Enabled() {
		go jobs.StartInstrumentSyncLoop(ctx, store.New(pool), mes, cfg.InstrumentSyncInterval)
	}

	auditStore := auditlog.NewStore(pool)
	router := handlers.NewRouter(handlers.RouterDeps{
		Cfg:   cfg,
		Pool:  pool,
		Pub:   pub,
		Audit: auditStore,
		SCM:   scm,
		MES:   mes,
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
