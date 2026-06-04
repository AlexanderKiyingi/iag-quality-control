package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"iag-quality-control/backend/internal/config"
	"iag-quality-control/backend/internal/events"
)

func main() {
	cfg := config.Load()
	pub := events.NewPublisher(cfg.KafkaBrokers, cfg.KafkaTopic, cfg.KafkaClientID)
	defer pub.Close()

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "service": cfg.ServiceName})
	})
	r.GET("/ready", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	})

	v1 := r.Group("/api/v1")
	{
		v1.POST("/lab/results", func(c *gin.Context) {
			var body struct {
				BatchBusinessID string  `json:"batch_business_id" binding:"required"`
				Moisture        float64 `json:"moisture"`
				CupScore        float64 `json:"cup_score"`
				Grade           string  `json:"grade"`
				Tester          string  `json:"tester"`
			}
			if err := c.ShouldBindJSON(&body); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			data := map[string]any{
				"batch_business_id": body.BatchBusinessID,
				"moisture":          body.Moisture,
				"cup_score":         body.CupScore,
				"grade":             body.Grade,
				"tester":            body.Tester,
			}
			if err := pub.Publish(c.Request.Context(), "qc.lab.result_recorded", data); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "kafka publish failed"})
				return
			}
			c.JSON(http.StatusCreated, gin.H{"status": "published", "event_type": "qc.lab.result_recorded"})
		})

		v1.POST("/coa", func(c *gin.Context) {
			var body struct {
				LotBusinessID string `json:"lot_business_id" binding:"required"`
				CoaNumber     string `json:"coa_number" binding:"required"`
				DocumentRef   string `json:"document_ref"`
			}
			if err := c.ShouldBindJSON(&body); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			data := map[string]any{
				"lot_business_id": body.LotBusinessID,
				"coa_number":      body.CoaNumber,
				"document_ref":    body.DocumentRef,
			}
			if err := pub.Publish(c.Request.Context(), "qc.coa.issued", data); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "kafka publish failed"})
				return
			}
			c.JSON(http.StatusCreated, gin.H{"status": "published", "event_type": "qc.coa.issued"})
		})
	}

	srv := &http.Server{Addr: ":" + cfg.Port, Handler: r, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		log.Printf("quality-control listening on :%s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}
