package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"iag-quality-control/backend/internal/auditlog"
	"iag-quality-control/backend/internal/config"
	"iag-quality-control/backend/internal/events"
	"iag-quality-control/backend/internal/middleware"
)

type RouterDeps struct {
	Cfg   config.Config
	Pub   *events.Publisher
	Audit *auditlog.MemoryStore
}

func NewRouter(deps RouterDeps) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.RequestAudit(deps.Audit))

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "service": deps.Cfg.ServiceName})
	})
	r.GET("/ready", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	})

	qc := &QC{Pub: deps.Pub}
	admin := &Admin{Cfg: deps.Cfg, Audit: deps.Audit}

	v1 := r.Group("/api/v1")
	{
		v1.POST("/samples", qc.PostSample)
		v1.POST("/lab/results", qc.PostLabResult)
		v1.POST("/coa", qc.PostCoA)

		adm := v1.Group("/admin", middleware.RequireBearer())
		{
			adm.GET("/audit-logs", admin.ListAPIAuditLogs)
			adm.GET("/monitoring/summary", admin.MonitoringSummary)
			adm.GET("/monitoring/activity", admin.MonitoringActivity)
		}
	}
	return r
}
