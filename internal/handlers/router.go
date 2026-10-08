package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"

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
	r.Use(otelgin.Middleware(deps.Cfg.ServiceName))
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
			"status":   "ready",
			"database": true,
			"kafka":    deps.Pub.Enabled(),
			"scm":      deps.SCM != nil && deps.SCM.Enabled(),
			"mes":      deps.MES != nil && deps.MES.Enabled(),
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
		v1.GET("/analytics/spc/parameters", middleware.RequirePermission("qc.view_analytics"), qc.SPCParameters)

		v1.GET("/reports/day-summary", middleware.RequirePermission("qc.view_reports"), qc.DayReport)
		v1.GET("/reports/day-summary/pdf", middleware.RequirePermission("qc.export_pdf"), qc.DayReportPDF)
		v1.GET("/reports/trends", middleware.RequirePermission("qc.view_reports"), qc.TrendsReport)
		v1.GET("/reports/weekly-summary", middleware.RequirePermission("qc.view_reports"), qc.WeeklySummary)
		v1.GET("/reports/export", middleware.RequirePermission("qc.export_pdf"), qc.ExportReport)
		v1.GET("/reports/audit-pack", middleware.RequirePermission("qc.export_audit_pack"), qc.AuditPack)

		v1.GET("/batches", middleware.RequirePermission("qc.view_scm_context"), qc.ListBatches)
		// Same wildcard name as the sibling routes: gin panics at boot on
		// "/batches/:batchId/lab" next to "/batches/:businessId/pipeline".
		v1.GET("/batches/:businessId/lab", middleware.RequirePermission("qc.view_lab_summary"), qc.GetBatchLab)
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
		v1.GET("/cupping-sessions/:id/scores", middleware.RequirePermission("qc.view_lab_summary"), qc.GetCuppingPanel)
		v1.POST("/cupping-sessions/:id/scores", middleware.RequirePermission("qc.record_tests"), qc.SaveCuppingScore)
		v1.GET("/cupping-scores", middleware.RequirePermission("qc.view_lab_summary"), qc.ListCuppingScores)

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

		// Parameterised measurements (013). /lab/results above is a six-metric
		// batch rollup, not a generic result endpoint; this is the one that can
		// record an arbitrary analyte. Recording is nested under the sample like
		// the other test routes, so it reuses the same ":id" wildcard name.
		v1.GET("/lab/measurements", middleware.RequirePermission("qc.view_lab_summary"), qc.ListLabMeasurements)
		v1.POST("/samples/:id/measurements", middleware.RequirePermission("qc.record_tests"), qc.PostLabMeasurement)

		v1.GET("/certification/pending", middleware.RequirePermission("qc.approve_certification"), qc.ListPendingCertifications)
		v1.POST("/certification/requests", middleware.RequirePermission("qc.request_certification"), qc.CreateCertificationRequest)
		v1.GET("/certification/requests/:id", middleware.RequirePermission("qc.request_certification"), qc.GetCertificationRequest)
		v1.POST("/certification/requests/:id/approve", middleware.RequirePermission("qc.approve_certification"), qc.ApproveCertification)

		v1.GET("/coa", middleware.RequirePermission("qc.view_coa"), qc.ListCoA)
		v1.GET("/coa/:coaNumber/pdf", middleware.RequirePermission("qc.export_pdf"), qc.CoAPDF)
		v1.GET("/coa/:coaNumber", middleware.RequirePermission("qc.view_coa"), qc.GetCoAByNumber)
		v1.POST("/coa", middleware.RequirePermission("qc.issue_coa"), qc.PostCoA)

		// Lab methods and requests (010). Reuse the instrument permissions: the
		// people who keep the instrument register keep the method register.
		v1.GET("/lab/methods", middleware.RequirePermission("qc.view_instruments"), qc.ListLabMethods)
		v1.POST("/lab/methods", middleware.RequirePermission("qc.change_instruments"), qc.UpsertLabMethod)
		v1.GET("/lab/requests", middleware.RequirePermission("qc.view_samples"), qc.ListLabRequests)
		v1.POST("/lab/requests", middleware.RequirePermission("qc.add_sample"), qc.UpsertLabRequest)
		// Calibrations and stability studies (011). Each is its own top-level
		// collection so its ":id" sits in a fresh position: a second wildcard
		// NAME under /instruments (e.g. /instruments/:instrumentId/calibrations
		// beside the existing /instruments/:id) is what makes gin panic, which
		// is what TestNewRouterBuilds guards. History is read as
		// GET /calibrations?instrument=INS-26-0001.
		//
		// Permissions are reused rather than added: whoever keeps the instrument
		// register records its calibrations, and a stability study is lab work
		// planning like a lab request.
		v1.GET("/calibrations", middleware.RequirePermission("qc.view_instruments"), qc.ListCalibrations)
		v1.POST("/calibrations", middleware.RequirePermission("qc.change_instruments"), qc.CreateCalibration)
		v1.GET("/calibrations/:id", middleware.RequirePermission("qc.view_instruments"), qc.GetCalibration)
		v1.GET("/stability-studies", middleware.RequirePermission("qc.view_samples"), qc.ListStabilityStudies)
		v1.POST("/stability-studies", middleware.RequirePermission("qc.add_sample"), qc.UpsertStabilityStudy)

		// QA registers (012). Same reasoning as above: fresh top-level segments,
		// one wildcard name (":id") per position.
		//
		// Non-conformances and in-process checks reuse the compliance
		// permissions — they are the compliance officer's registers. Release
		// decisions and the hold log get the one new pair, because releasing or
		// holding a batch emits events other services act on and must be
		// grantable apart from data entry.
		v1.GET("/non-conformances", middleware.RequirePermission("qc.view_compliance"), qc.ListNonConformances)
		v1.POST("/non-conformances", middleware.RequirePermission("qc.change_compliance"), qc.UpsertNonConformance)
		v1.GET("/non-conformances/:id", middleware.RequirePermission("qc.view_compliance"), qc.GetNonConformance)
		// Deletes exist only for planning data that has not been acted on; the
		// store refuses anything else with a 409 naming the alternative. The
		// quality record — samples, tests, cupping, measurements, calibrations,
		// CoAs, custody, holds, release decisions, CAPAs, NCs — has no DELETE
		// on purpose.
		v1.DELETE("/lab/methods/:id", middleware.RequirePermission("qc.change_instruments"), qc.deleteFrom("qc_lab_methods"))
		v1.DELETE("/lab/requests/:id", middleware.RequirePermission("qc.add_sample"), qc.deleteFrom("qc_lab_requests"))
		v1.DELETE("/stability-studies/:id", middleware.RequirePermission("qc.add_sample"), qc.deleteFrom("qc_stability_studies"))
		v1.DELETE("/in-process-checks/:id", middleware.RequirePermission("qc.change_compliance"), qc.deleteFrom("qc_in_process_checks"))
		v1.DELETE("/incoming-inspections/:id", middleware.RequirePermission("qc.change_compliance"), qc.deleteFrom("qc_incoming_inspections"))
		v1.GET("/incoming-inspections", middleware.RequirePermission("qc.view_compliance"), qc.ListIncomingInspections)
		v1.POST("/incoming-inspections", middleware.RequirePermission("qc.change_compliance"), qc.UpsertIncomingInspection)
		v1.GET("/in-process-checks", middleware.RequirePermission("qc.view_compliance"), qc.ListInProcessChecks)
		v1.POST("/in-process-checks", middleware.RequirePermission("qc.change_compliance"), qc.UpsertInProcessCheck)
		v1.GET("/release-decisions", middleware.RequirePermission("qc.view_release_decisions"), qc.ListReleaseDecisions)
		v1.POST("/release-decisions", middleware.RequirePermission("qc.decide_release"), qc.UpsertReleaseDecision)
		v1.GET("/hold-events", middleware.RequirePermission("qc.view_release_decisions"), qc.ListHoldEvents)
		v1.POST("/hold-events", middleware.RequirePermission("qc.decide_release"), qc.CreateHoldEvent)
		// Specification limits (016). Changing one gets its own permission: a
		// spec decides what is automatically raised as a non-conformance and
		// held, so it must be grantable apart from recording results.
		v1.GET("/specifications", middleware.RequirePermission("qc.view_specifications"), qc.ListSpecifications)
		v1.POST("/specifications", middleware.RequirePermission("qc.change_specifications"), qc.UpsertSpecification)
		v1.GET("/specifications/:id", middleware.RequirePermission("qc.view_specifications"), qc.GetSpecification)
		// Before/after history of the quality record (017). Read-only: the log
		// is append-only in the database itself.
		v1.GET("/change-log", middleware.RequirePermission("qc.view_change_log"), qc.ListChangeLog)
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
