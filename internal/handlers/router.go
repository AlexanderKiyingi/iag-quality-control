package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"iag-quality-control/backend/internal/auditlog"
	"iag-quality-control/backend/internal/clients"
	"iag-quality-control/backend/internal/config"
	"iag-quality-control/backend/internal/db"
	"iag-quality-control/backend/internal/events"
	"iag-quality-control/backend/internal/middleware"
	"iag-quality-control/backend/internal/store"

	"github.com/jackc/pgx/v5/pgxpool"
)

type RouterDeps struct {
	Cfg   config.Config
	Pool  *pgxpool.Pool
	Pub   *events.Publisher
	Audit *auditlog.Store
	SCM   *clients.SCM
	MES   *clients.MES
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
		if err := db.Ping(c.Request.Context(), deps.Pool); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "degraded", "database": false})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"status":    "ready",
			"database":  true,
			"kafka":     deps.Pub.Enabled(),
			"scm":       deps.SCM != nil && deps.SCM.Enabled(),
			"mes":       deps.MES != nil && deps.MES.Enabled(),
		})
	})

	qc := &QC{
		Cfg:   deps.Cfg,
		Store: store.New(deps.Pool),
		Pub:   deps.Pub,
		SCM:   deps.SCM,
		MES:   deps.MES,
	}
	admin := &Admin{Cfg: deps.Cfg, Audit: deps.Audit}

	v1 := r.Group("/api/v1")
	{
		v1.GET("/dashboard/summary", qc.DashboardSummary)
		v1.GET("/search", qc.Search)
		v1.GET("/calendar", qc.Calendar)
		v1.GET("/analytics/spc", qc.SPCAnalytics)

		v1.GET("/reports/day-summary", qc.DayReport)
		v1.GET("/reports/day-summary/pdf", qc.DayReportPDF)
		v1.GET("/reports/trends", qc.TrendsReport)
		v1.GET("/reports/weekly-summary", qc.WeeklySummary)
		v1.GET("/reports/export", qc.ExportReport)
		v1.GET("/reports/audit-pack", qc.AuditPack)

		v1.GET("/batches", qc.ListBatches)
		v1.GET("/batches/:batchId/lab", qc.GetBatchLab)
		v1.GET("/batches/:businessId/pipeline", qc.GetBatchPipeline)
		v1.GET("/batches/:businessId", qc.GetBatch)
		v1.GET("/export-lots", qc.ListExportLots)
		v1.GET("/export-lots/:businessId", qc.GetExportLot)
		v1.GET("/farmers", qc.ListFarmers)
		v1.GET("/farmers/:businessId", qc.GetFarmer)

		v1.GET("/technicians", qc.ListTechnicians)
		v1.POST("/technicians", qc.UpsertTechnician)
		v1.GET("/technicians/:id", qc.GetTechnician)

		v1.GET("/physical-tests", qc.ListPhysicalTests)
		v1.GET("/chemical-tests", qc.ListChemicalTests)
		v1.GET("/cupping-sessions", qc.ListCuppingSessions)

		v1.GET("/queues/instrument", qc.InstrumentQueue)
		v1.GET("/queues/hplc", qc.HPLCQueue)
		v1.GET("/queues/cupping", qc.CuppingQueue)

		v1.GET("/external-audits", qc.ListExternalAudits)
		v1.POST("/external-audits", qc.UpsertExternalAudit)

		v1.GET("/samples", qc.ListSamples)
		v1.POST("/samples", qc.PostSample)
		v1.GET("/samples/:id/detail", qc.GetSampleDetail)
		v1.GET("/samples/:id/label", qc.SampleLabel)
		v1.GET("/samples/:id/cupping", qc.ListSampleCupping)
		v1.GET("/samples/:id/cupping/pdf", qc.CuppingPDF)
		v1.GET("/samples/:id/custody", qc.ListCustodyLogs)
		v1.POST("/samples/:id/custody", qc.CreateCustodyLog)
		v1.GET("/samples/:id", qc.GetSample)
		v1.PATCH("/samples/:id", qc.PatchSample)
		v1.POST("/samples/:id/physical-tests", qc.PostPhysicalTest)
		v1.POST("/samples/:id/chemical-tests", qc.PostChemicalTest)
		v1.POST("/samples/:id/cupping", qc.PostCupping)

		v1.POST("/lab/results", qc.PostLabResult)

		v1.GET("/certification/pending", qc.ListPendingCertifications)
		v1.POST("/certification/requests", qc.CreateCertificationRequest)
		v1.GET("/certification/requests/:id", qc.GetCertificationRequest)
		v1.POST("/certification/requests/:id/approve", qc.ApproveCertification)

		v1.GET("/coa", qc.ListCoA)
		v1.GET("/coa/:coaNumber/pdf", qc.CoAPDF)
		v1.GET("/coa/:coaNumber", qc.GetCoAByNumber)
		v1.POST("/coa", qc.PostCoA)

		v1.GET("/instruments", qc.ListInstruments)
		v1.POST("/instruments", qc.UpsertInstrument)
		v1.POST("/instruments/sync", qc.SyncInstruments)
		v1.GET("/instruments/:id", qc.GetInstrument)

		v1.GET("/compliance/logs", qc.ListComplianceLogs)
		v1.POST("/compliance/logs", qc.CreateComplianceLog)
		v1.GET("/compliance/capas", qc.ListCAPAs)
		v1.POST("/compliance/capas", qc.UpsertCAPA)

		adm := v1.Group("/admin", middleware.RequireBearer())
		{
			adm.GET("/audit-logs", admin.ListAPIAuditLogs)
			adm.GET("/monitoring/summary", admin.MonitoringSummary)
			adm.GET("/monitoring/activity", admin.MonitoringActivity)
		}
	}
	return r
}
