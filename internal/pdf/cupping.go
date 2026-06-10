package pdf

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/jung-kurt/gofpdf"
)

type CuppingData struct {
	SessionID       string
	SampleID        string
	BatchBusinessID string
	SessionDate     string
	Scorers         []string
	Fragrance       float64
	Flavor          float64
	Aftertaste      float64
	Acidity         float64
	Body            float64
	Balance         float64
	Uniformity      float64
	CleanCup        float64
	Sweetness       float64
	Overall         float64
	DefectCat1      int
	DefectCat2      int
	TotalScore      float64
	Grade           string
	Notes           string
}

func RenderCupping(d CuppingData) ([]byte, error) {
	var buf bytes.Buffer
	return writePDF(&buf, func(pdf *gofpdf.Fpdf) {
		heading(pdf, "SCA CUPPING FORM")
		line(pdf, "Session:", d.SessionID)
		line(pdf, "Sample:", d.SampleID)
		line(pdf, "Batch:", d.BatchBusinessID)
		line(pdf, "Date:", d.SessionDate)
		if len(d.Scorers) > 0 {
			line(pdf, "Scorers:", strings.Join(d.Scorers, ", "))
		}
		pdf.Ln(4)
		pdf.SetFont("Arial", "B", 12)
		pdf.Cell(0, 8, "Attribute Scores")
		pdf.Ln(9)
		attrs := []struct{ label string; val float64 }{
			{"Fragrance/Aroma", d.Fragrance},
			{"Flavor", d.Flavor},
			{"Aftertaste", d.Aftertaste},
			{"Acidity", d.Acidity},
			{"Body", d.Body},
			{"Balance", d.Balance},
			{"Uniformity", d.Uniformity},
			{"Clean Cup", d.CleanCup},
			{"Sweetness", d.Sweetness},
			{"Overall", d.Overall},
		}
		for _, a := range attrs {
			line(pdf, a.label+":", fmt.Sprintf("%.1f", a.val))
		}
		pdf.Ln(2)
		line(pdf, "Defect Cat 1:", fmt.Sprintf("%d", d.DefectCat1))
		line(pdf, "Defect Cat 2:", fmt.Sprintf("%d", d.DefectCat2))
		line(pdf, "Total Score:", fmt.Sprintf("%.1f", d.TotalScore))
		line(pdf, "Grade:", d.Grade)
		if d.Notes != "" {
			pdf.Ln(2)
			pdf.SetFont("Arial", "B", 11)
			pdf.Cell(0, 7, "Tasting Notes")
			pdf.Ln(7)
			pdf.SetFont("Arial", "", 10)
			pdf.MultiCell(0, 5, d.Notes, "", "L", false)
		}
		footer(pdf, "SCA cupping protocol — IAG Quality Control")
	})
}
