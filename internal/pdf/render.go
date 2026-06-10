package pdf

import (
	"bytes"
	"fmt"

	"github.com/jung-kurt/gofpdf"
)

func writePDF(buf *bytes.Buffer, build func(pdf *gofpdf.Fpdf)) ([]byte, error) {
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(18, 18, 18)
	pdf.AddPage()
	build(pdf)
	if err := pdf.Output(buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func heading(pdf *gofpdf.Fpdf, title string) {
	pdf.SetFont("Arial", "B", 16)
	pdf.Cell(0, 10, title)
	pdf.Ln(12)
}

func line(pdf *gofpdf.Fpdf, label, value string) {
	pdf.SetFont("Arial", "", 11)
	pdf.Cell(55, 7, label)
	pdf.SetFont("Arial", "B", 11)
	pdf.Cell(0, 7, value)
	pdf.Ln(7)
}

func footer(pdf *gofpdf.Fpdf, text string) {
	pdf.Ln(8)
	pdf.SetFont("Arial", "I", 9)
	pdf.MultiCell(0, 5, text, "", "L", false)
}

func fmtFloat(v *float64, suffix string) string {
	if v == nil {
		return "—"
	}
	if suffix != "" {
		return fmt.Sprintf("%.2f %s", *v, suffix)
	}
	return fmt.Sprintf("%.2f", *v)
}
