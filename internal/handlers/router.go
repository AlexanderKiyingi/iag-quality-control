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
	"iag-quality-control/backend/internal/outbox"
	"iag-quality-control/backend/internal/store"

	"github.com/jackc/pgx/v5/pgxpool"
)

type RouterDeps struct {
	Cfg          config.Config
	Pool         *pgxpool.Pool
	Pub          *events.Publisher
	Outbox       *outbox.Store
	Audit        *auditlog.Store
	SCM          *clients.SCM
	MES          *clients.MES
	PlatformAuth *middleware.PlatformAuth
	StrictRBAC   bool
}

func NewRouter(deps RouterDeps) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	if deps.PlatformAuth != nil {
		r.Use(deps.PlatformAuth.AttachPrincipal())
	}
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
		Cfg:    deps.Cfg,
		Store:  store.New(deps.Pool),
		Pub:    deps.Pub,
		Outbox: deps.Outbox,
		SCM:    deps.SCM,
		MES:    deps.MES,
	}
	admin := &Admin{Cfg: deps.Cfg, Audit: deps.Audit}

	v1 := r.Group("/api/v1")
	if deps.PlatformAuth != nil {
		v1.Use(deps.PlatformAuth.RequireAuth())
	}
	if deps.StrictRBAC {
		v1.Use(middleware.StrictRBAC())
	}
	{
		v1.GET("/dashboard/summary", middleware.RequirePermission("qc.view_reports"), qc.DashboardSummary)
		v1.GET("/search", middleware.RequirePermission("qc.search"), qc.Search)
		v1.GET("/calendar", middleware.RequirePermission("qc.view_calendar"), qc.Calendar)
		v1.GET("/analytics/spc", middleware.RequirePermission("qc.view_analytics"), qc.SPCAnalytics)

		v1.GET("/reports/day-summary", middleware.RequirePermission("qc.view_reports"), qc.DayReport)
		v1.GET("/reports/day-summary/pdf", middleware.RequirePermission("qc.export_pdf"), qc.DayReportPDF)
		v1.GET("/reports/trends", middleware.RequirePermission("qc.view_reports"), qc.TrendsReport)
		v1.GET("/reports/weekly-summary", middleware.RequirePermission("qc.view_reports"), qc.WeeklySummary)
		v1.GET("/reports/export", middleware.RequirePermission("qc.export_pdf"), qc.ExportReport)
		v1.GET("/reports/audit-pack", middleware.RequirePermission("qc.export_audit_pack"), qc.AuditPack)

		v1.GET("/batches", middleware.RequirePermission("qc.view_scm_context"), qc.ListBatches)
		v1.GET("/batches/:batchId/lab", middleware.RequirePermission("qc.view_lab_summary"), qc.GetBatchLab)
		v1.GET("/batches/:businessId/pipeline", middleware.RequirePermission("qc.view_lab_summary"), qc.GetBatchPipeline)
		v1.GET("/batches/:businessId", middleware.RequirePermission("qc.view_scm_context"), qc.GetBatch)
		v1.GET("/export-lots", middleware.RequirePermission("qc.view_scm_context"), qc.ListExportLots)
		v1.GET("/export-lots/:businessId", middleware.RequirePermission("qc.view_scm_context"), qc.GetExportLot)
		v1.GET("/farmers", middleware.RequirePermission("qc.view_scm_context"), qc.ListFarmers)
		v1.GET("/farmers/:businessId", middleware.RequirePermission("qc.view_scm_context"), qc.GetFarmer)

		v1.GET("/technicians", middleware.RequirePermission("qc.view_technicians"), qc.ListTechnicians)
		v1.POST("/technicians", middleware.RequirePermission("qc.change_technicians"), qc.UpsertTechnician)
		v1.GET("/technicians/:id", middleware.RequirePermission("qc.view_technicians"), qc.GetTechnician)

		v1.GET("/physical-tests", middleware.RequirePermission("qc.view_lab_summary"), qc.ListPhysicalTests)
		v1.GET("/chemical-tests", middleware.RequirePermission("qc.view_lab_summary"), qc.ListChemicalTests)
		v1.GET("/cupping-sessions", middleware.RequirePermission("qc.view_lab_summary"), qc.ListCuppingSessions)

		v1.GET("/queues/instrument", middleware.RequirePermission("qc.view_queues"), qc.InstrumentQueue)
		v1.GET("/queues/hplc", middleware.RequirePermission("qc.view_queues"), qc.HPLCQueue)
		v1.GET("/queues/cupping", middleware.RequirePermission("qc.view_queues"), qc.CuppingQueue)

		v1.GET("/external-audits", middleware.RequirePermission("qc.view_external_audits"), qc.ListExternalAudits)
		v1.POST("/external-audits", middleware.RequirePermission("qc.change_external_audits"), qc.UpsertExternalAudit)

		v1.GET("/samples", middleware.RequirePermission("qc.view_samples"), qc.ListSamples)
		v1.POST("/samples", middleware.RequirePermission("qc.add_sample"), qc.PostSample)
		v1.GET("/samples/:id/detail", middleware.RequirePermission("qc.view_samples"), qc.GetSampleDetail)
		v1.GET("/samples/:id/label", middleware.RequirePermission("qc.print_labels"), qc.SampleLabel)
		v1.GET("/samples/:id/cupping", middleware.RequirePermission("qc.view_samples"), qc.ListSampleCupping)
		v1.GET("/samples/:id/cupping/pdf", middleware.RequirePermission("qc.export_pdf"), qc.CuppingPDF)
		v1.GET("/samples/:id/custody", middleware.RequirePermission("qc.view_custody"), qc.ListCustodyLogs)
		v1.POST("/samples/:id/custody", middleware.RequirePermission("qc.change_custody"), qc.CreateCustodyLog)
		v1.GET("/samples/:id", middleware.RequirePermission("qc.view_samples"), qc.GetSample)
		v1.PATCH("/samples/:id", middleware.RequirePermission("qc.change_sample"), qc.PatchSample)
		v1.POST("/samples/:id/physical-tests", middleware.RequirePermission("qc.record_tests"), qc.PostPhysicalTest)
		v1.POST("/samples/:id/chemical-tests", middleware.RequirePermission("qc.record_tests"), qc.PostChemicalTest)
		v1.POST("/samples/:id/cupping", middleware.RequirePermission("qc.record_tests"), qc.PostCupping)

		v1.POST("/lab/results", middleware.RequirePermission("qc.record_tests"), qc.PostLabResult)

		v1.GET("/certification/pending", middleware.RequirePermission("qc.approve_certification"), qc.ListPendingCertifications)
		v1.POST("/certification/requests", middleware.RequirePermission("qc.request_certification"), qc.CreateCertificationRequest)
		v1.GET("/certification/requests/:id", middleware.RequirePermission("qc.request_certification"), qc.GetCertificationRequest)
		v1.POST("/certification/requests/:id/approve", middleware.RequirePermission("qc.approve_certification"), qc.ApproveCertification)

		v1.GET("/coa", middleware.RequirePermission("qc.view_coa"), qc.ListCoA)
		v1.GET("/coa/:coaNumber/pdf", middleware.RequirePermission("qc.export_pdf"), qc.CoAPDF)
		v1.GET("/coa/:coaNumber", middleware.RequirePermission("qc.view_coa"), qc.GetCoAByNumber)
		v1.POST("/coa", middleware.RequirePermission("qc.issue_coa"), qc.PostCoA)

		v1.GET("/instruments", middleware.RequirePermission("qc.view_instruments"), qc.ListInstruments)
		v1.POST("/instruments", middleware.RequirePermission("qc.change_instruments"), qc.UpsertInstrument)
		v1.POST("/instruments/sync", middleware.RequirePermission("qc.sync_instruments"), qc.SyncInstruments)
		v1.GET("/instruments/:id", middleware.RequirePermission("qc.view_instruments"), qc.GetInstrument)

		v1.GET("/compliance/logs", middleware.RequirePermission("qc.view_compliance"), qc.ListComplianceLogs)
		v1.POST("/compliance/logs", middleware.RequirePermission("qc.change_compliance"), qc.CreateComplianceLog)
		v1.GET("/compliance/capas", middleware.RequirePermission("qc.view_compliance"), qc.ListCAPAs)
		v1.POST("/compliance/capas", middleware.RequirePermission("qc.change_compliance"), qc.UpsertCAPA)

		adm := v1.Group("/admin", middleware.RequireStaff(), middleware.RequirePermission("qc.admin.read"))
		{
			adm.GET("/audit-logs", admin.ListAPIAuditLogs)
			adm.GET("/monitoring/summary", admin.MonitoringSummary)
			adm.GET("/monitoring/activity", admin.MonitoringActivity)
		}
	}
	return r
}
