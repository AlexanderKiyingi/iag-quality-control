package labels

import (
	"fmt"
	"strings"
)

type SampleLabel struct {
	SampleID        string `json:"sample_id"`
	BatchBusinessID string `json:"batch_business_id"`
	SampleType      string `json:"sample_type"`
	BarcodeValue    string `json:"barcode_value"`
}

func NewSampleLabel(sampleID, batchID, sampleType string) SampleLabel {
	return SampleLabel{
		SampleID:        sampleID,
		BatchBusinessID: batchID,
		SampleType:      sampleType,
		BarcodeValue:    BarcodeValue(sampleID),
	}
}

func BarcodeValue(sampleID string) string {
	return strings.TrimSpace(sampleID)
}

func RenderZPL(l SampleLabel) string {
	payload := escapeZPL(l.BarcodeValue)
	batch := escapeZPL(l.BatchBusinessID)
	stype := escapeZPL(l.SampleType)
	return fmt.Sprintf(`^XA
^FO40,30^A0N,28,28^FDCUPPA LIMS Sample^FS
^FO40,70^BY2,3,80^BCN,80,Y,N,N^FD%s^FS
^FO40,175^A0N,24,24^FD%s^FS
^FO40,210^A0N,20,20^FDBatch: %s^FS
^FO40,240^A0N,20,20^FDType: %s^FS
^XZ
`, payload, payload, batch, stype)
}

func RenderSVG(l SampleLabel) string {
	bars := barcodeBars(l.BarcodeValue)
	width := len(bars)*3 + 40
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="120" viewBox="0 0 %d 120">
  <rect width="100%%" height="100%%" fill="#fff"/>
  <text x="20" y="18" font-family="monospace" font-size="11" fill="#333">%s</text>
  %s
  <text x="20" y="110" font-family="monospace" font-size="12" fill="#000">%s</text>
</svg>`, width, width, escapeXML(l.SampleType), bars, escapeXML(l.BarcodeValue))
}

func barcodeBars(payload string) string {
	if payload == "" {
		return ""
	}
	var b strings.Builder
	x := 20
	for i, r := range payload {
		w := 1 + int(r%3)
		if i%2 == 0 {
			b.WriteString(fmt.Sprintf(`<rect x="%d" y="28" width="%d" height="60" fill="#000"/>`, x, w*2))
		}
		x += w*2 + 1
	}
	return b.String()
}

func escapeZPL(s string) string {
	s = strings.ReplaceAll(s, "^", "")
	s = strings.ReplaceAll(s, "~", "")
	return s
}

func escapeXML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	return s
}
