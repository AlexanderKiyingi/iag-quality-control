package pdf

import (
	"bytes"
	"fmt"

	"github.com/jung-kurt/gofpdf"
)

type DayReportData struct {
	ReportDate            string
	SamplesReceived       int
	SamplesCompleted      int
	PhysicalTests         int
	ChemicalTests         int
	CuppingSessions       int
	CoAsIssued            int
	CertificationsPending int
	OpenCAPAs             int
	OverdueCalibrations   int
	AvgCupScore           *float64
	AvgMoisture           *float64
}

func RenderDayReport(d DayReportData) ([]byte, error) {
	var buf bytes.Buffer
	return writePDF(&buf, func(pdf *gofpdf.Fpdf) {
		heading(pdf, "LAB DAY REPORT")
		line(pdf, "Report Date:", d.ReportDate)
		pdf.Ln(4)
		pdf.SetFont("Arial", "B", 12)
		pdf.Cell(0, 8, "Throughput")
		pdf.Ln(9)
		line(pdf, "Samples Received:", fmt.Sprintf("%d", d.SamplesReceived))
		line(pdf, "Samples Completed:", fmt.Sprintf("%d", d.SamplesCompleted))
		line(pdf, "Physical Tests:", fmt.Sprintf("%d", d.PhysicalTests))
		line(pdf, "Chemical Tests:", fmt.Sprintf("%d", d.ChemicalTests))
		line(pdf, "Cupping Sessions:", fmt.Sprintf("%d", d.CuppingSessions))
		line(pdf, "CoAs Issued:", fmt.Sprintf("%d", d.CoAsIssued))
		pdf.Ln(2)
		pdf.SetFont("Arial", "B", 12)
		pdf.Cell(0, 8, "Quality & Compliance")
		pdf.Ln(9)
		line(pdf, "Certifications Pending:", fmt.Sprintf("%d", d.CertificationsPending))
		line(pdf, "Open CAPAs:", fmt.Sprintf("%d", d.OpenCAPAs))
		line(pdf, "Overdue Calibrations:", fmt.Sprintf("%d", d.OverdueCalibrations))
		line(pdf, "Avg Cup Score:", fmtFloat(d.AvgCupScore, "pts"))
		line(pdf, "Avg Moisture:", fmtFloat(d.AvgMoisture, "%"))
		footer(pdf, "Daily lab summary — IAG Quality Control")
	})
}
