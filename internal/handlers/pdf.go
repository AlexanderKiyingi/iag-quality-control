package handlers

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"iag-quality-control/backend/internal/pdf"
)

func writePDFResponse(c *gin.Context, filename string, data []byte, err error) {
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "pdf render failed"})
		return
	}
	c.Header("Content-Type", "application/pdf")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Data(http.StatusOK, "application/pdf", data)
}

func (h *QC) CoAPDF(c *gin.Context) {
	coa, err := h.Store.GetCoAByNumber(c.Request.Context(), c.Param("coaNumber"))
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load coa"})
		return
	}
	payload := pdf.CoAData{
		CoaNumber:       coa.CoaNumber,
		LotBusinessID:   coa.LotBusinessID,
		BatchBusinessID: coa.BatchBusinessID,
		DocumentRef:     coa.DocumentRef,
		IssuedBy:        coa.IssuedBy,
		IssuedAt:        coa.IssuedAt.UTC().Format("2006-01-02 15:04 UTC"),
		Status:          coa.Status,
	}
	if coa.BatchBusinessID != "" {
		if summary, err := h.Store.GetBatchLabSummary(c.Request.Context(), coa.BatchBusinessID); err == nil {
			payload.Moisture = summary.Moisture
			payload.CupScore = summary.CupScore
			payload.Grade = summary.Grade
		}
	}
	raw, err := pdf.RenderCoA(payload)
	writePDFResponse(c, safeFilename(coa.CoaNumber)+".pdf", raw, err)
}

func (h *QC) CuppingPDF(c *gin.Context) {
	sampleID := c.Param("id")
	session, err := h.Store.GetLatestCuppingBySample(c.Request.Context(), sampleID)
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load cupping session"})
		return
	}
	raw, err := pdf.RenderCupping(pdf.CuppingData{
		SessionID:       session.BusinessID,
		SampleID:        session.SampleBusinessID,
		BatchBusinessID: session.BatchBusinessID,
		SessionDate:     session.SessionDate,
		Scorers:         session.Scorers,
		Fragrance:       session.Fragrance,
		Flavor:          session.Flavor,
		Aftertaste:      session.Aftertaste,
		Acidity:         session.Acidity,
		Body:            session.Body,
		Balance:         session.Balance,
		Uniformity:      session.Uniformity,
		CleanCup:        session.CleanCup,
		Sweetness:       session.Sweetness,
		Overall:         session.Overall,
		DefectCat1:      session.DefectCat1,
		DefectCat2:      session.DefectCat2,
		TotalScore:      session.TotalScore,
		Grade:           session.Grade,
		Notes:           session.Notes,
	})
	writePDFResponse(c, safeFilename(sampleID)+"-cupping.pdf", raw, err)
}

func (h *QC) DayReportPDF(c *gin.Context) {
	report, err := h.Store.DayReport(c.Request.Context(), c.Query("date"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "report failed"})
		return
	}
	raw, err := pdf.RenderDayReport(pdf.DayReportData{
		ReportDate:            report.ReportDate,
		SamplesReceived:       report.SamplesReceived,
		SamplesCompleted:      report.SamplesCompleted,
		PhysicalTests:         report.PhysicalTests,
		ChemicalTests:         report.ChemicalTests,
		CuppingSessions:       report.CuppingSessions,
		CoAsIssued:            report.CoAsIssued,
		CertificationsPending: report.CertificationsPending,
		OpenCAPAs:             report.OpenCAPAs,
		OverdueCalibrations:   report.OverdueCalibrations,
		AvgCupScore:           report.AvgCupScore,
		AvgMoisture:           report.AvgMoisture,
	})
	writePDFResponse(c, "qc-day-report-"+report.ReportDate+".pdf", raw, err)
}

func safeFilename(s string) string {
	s = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, s)
	if s == "" {
		return "document"
	}
	return s
}
