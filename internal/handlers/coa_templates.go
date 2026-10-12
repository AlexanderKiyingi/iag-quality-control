package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"iag-quality-control/backend/internal/pdf"
	"iag-quality-control/backend/internal/store"
)

/*
coaLayout fills a template with a batch's latest results. Where the template
asks for the specification, it is the active finished-goods spec for the
parameter (grade-specific when the batch has a grade), and the verdict is the
result judged against it now.
*/
func (h *QC) coaLayout(ctx context.Context, t store.CoATemplate, batch, customer, grade string) (*pdf.CoALayout, error) {
	results := map[string]store.BatchResult{}
	if batch != "" {
		var err error
		if results, err = h.Store.BatchResults(ctx, batch); err != nil {
			return nil, err
		}
	}
	layout := &pdf.CoALayout{Title: t.Title, Customer: customer, HeaderNote: t.HeaderNote,
		FooterStatement: t.FooterStatement, ShowSignatures: t.ShowSignatures}
	if layout.Customer == "" {
		layout.Customer = t.CustomerRef
	}
	for _, f := range t.Parameters {
		row := pdf.CoARow{Label: f.Label, Unit: f.Unit}
		r, tested := results[f.Parameter]
		if tested {
			row.Result = strconv.FormatFloat(r.Value, 'f', -1, 64)
			if row.Unit == "" {
				row.Unit = r.Unit
			}
		}
		if f.ShowSpec {
			spec, err := h.Store.ResolveSpec(ctx, "finished", f.Parameter, grade)
			switch {
			case err == nil:
				row.Spec = specBand(spec.LSL, spec.USL)
				if tested {
					row.Verdict = "pass"
					if (spec.LSL != nil && r.Value < *spec.LSL) || (spec.USL != nil && r.Value > *spec.USL) {
						row.Verdict = "fail"
					}
				}
			case !errors.Is(err, store.ErrNotFound):
				return nil, err
			default:
				row.Spec = "—"
			}
		}
		layout.Rows = append(layout.Rows, row)
	}
	return layout, nil
}

func specBand(lsl, usl *float64) string {
	f := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
	switch {
	case lsl != nil && usl != nil:
		return f(*lsl) + " – " + f(*usl)
	case usl != nil:
		return "≤ " + f(*usl)
	case lsl != nil:
		return "≥ " + f(*lsl)
	}
	return "—"
}

func (h *QC) ListCoATemplates(c *gin.Context) {
	items, err := h.Store.ListCoATemplates(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list templates"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

func (h *QC) GetCoATemplate(c *gin.Context) {
	item, err := h.Store.GetCoATemplate(c.Request.Context(), c.Param("id"))
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load template"})
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *QC) PutCoATemplate(c *gin.Context) {
	var body store.CoATemplateInput
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if id := c.Param("id"); id != "" {
		body.BusinessID = id
	}
	body.Actor = actorOf(c)
	item, err := h.Store.PutCoATemplate(c.Request.Context(), body)
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not save template"})
		return
	}
	c.JSON(http.StatusOK, item)
}

// PreviewCoATemplate prints a template against a batch, marked as a preview —
// what the customer would receive, before anything is issued or signed.
func (h *QC) PreviewCoATemplate(c *gin.Context) {
	t, err := h.Store.GetCoATemplate(c.Request.Context(), c.Param("id"))
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load template"})
		return
	}
	batch := strings.TrimSpace(c.Query("batch_business_id"))
	grade := ""
	if batch != "" {
		if summary, err := h.Store.GetBatchLabSummary(c.Request.Context(), batch); err == nil {
			grade = summary.Grade
		}
	}
	layout, err := h.coaLayout(c.Request.Context(), t, batch, t.CustomerRef, grade)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not gather the batch's results"})
		return
	}
	layout.Title = "PREVIEW — " + layout.Title
	raw, err := pdf.RenderCoA(pdf.CoAData{CoaNumber: "PREVIEW (not issued)", LotBusinessID: strings.TrimSpace(c.Query("lot_business_id")),
		BatchBusinessID: batch, IssuedBy: actorOf(c), IssuedAt: time.Now().UTC().Format("2006-01-02 15:04 UTC"), Template: layout})
	writePDFResponse(c, fmt.Sprintf("coa-preview-%s.pdf", safeFilename(t.BusinessID)), raw, err)
}

func (h *QC) ListAlertRoutes(c *gin.Context) {
	items, err := h.Store.ListAlertRoutes(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list alert routes"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

func (h *QC) UpdateAlertRoute(c *gin.Context) {
	var body store.AlertRoute
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	body.Kind = c.Param("kind")
	item, err := h.Store.UpdateAlertRoute(c.Request.Context(), body, actorOf(c))
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not update alert route"})
		return
	}
	c.JSON(http.StatusOK, item)
}
