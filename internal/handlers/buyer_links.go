package handlers

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"iag-quality-control/backend/internal/pdf"
	"iag-quality-control/backend/internal/store"
)

/*
Buyer links (027). QA shares one signed certificate with one buyer as a URL
that needs no account. The link is the credential, so:

  - only its hash is stored, and the token is shown once, when it is made;
  - it expires (30 days unless QA picks otherwise, never past 365);
  - every view is checked against the database, so revoking it, or the
    certificate being withdrawn, takes effect on the next request;
  - every view is logged for QA (time, page or PDF, coarse network);
  - the buyer sees only what the certificate's template prints — results,
    specifications, verdicts and signatures — never the batch, supplier,
    cupping notes or anything else internal.

The public routes sit outside /api/v1 (no bearer); the gateway serves them
at /api/v1/quality-control/public with its anonymous rate limit.
*/

// shareableCoA loads a certificate that may go to a buyer, with the payload
// it prints. A certificate with no template is refused: without one the
// legacy layout prints internal figures nobody chose to share.
func (h *QC) shareableCoA(ctx context.Context, coaNumber string) (store.CoA, pdf.CoAData, error) {
	coa, err := h.Store.CoAShareable(ctx, coaNumber)
	if err != nil {
		return coa, pdf.CoAData{}, err
	}
	payload, err := h.coaPayload(ctx, coa)
	if err != nil {
		return coa, payload, err
	}
	if payload.Template == nil {
		return coa, payload, errors.Join(store.ErrNotShareable,
			errors.New("no certificate template applies to it; set one up under CoA Templates"))
	}
	return coa, payload, nil
}

func respondNotShareable(c *gin.Context, err error) bool {
	if errors.Is(err, store.ErrNotShareable) {
		c.JSON(http.StatusConflict, gin.H{"error": strings.ReplaceAll(err.Error(), "\n", ": "), "code": "not_shareable"})
		return true
	}
	return false
}

// POST /coa/:coaNumber/buyer-links
func (h *QC) CreateBuyerLink(c *gin.Context) {
	var in store.BuyerLinkInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	coa, _, err := h.shareableCoA(c.Request.Context(), c.Param("coaNumber"))
	if respondStoreErr(c, err) || respondNotShareable(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if strings.TrimSpace(in.CustomerRef) == "" {
		in.CustomerRef = coa.CustomerRef
	}
	in.Actor = actorOf(c)
	link, err := h.Store.CreateBuyerLink(c.Request.Context(), coa.CoaNumber, in)
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create the link"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"item": link})
}

// GET /coa/:coaNumber/buyer-links
func (h *QC) ListBuyerLinks(c *gin.Context) {
	items, err := h.Store.ListBuyerLinks(c.Request.Context(), c.Param("coaNumber"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list links"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// GET /buyer-links/:id/views
func (h *QC) ListBuyerLinkViews(c *gin.Context) {
	link, err := h.Store.GetBuyerLink(c.Request.Context(), c.Param("id"))
	if respondStoreErr(c, err) {
		return
	}
	views, err := h.Store.ListBuyerLinkViews(c.Request.Context(), link.BusinessID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list views"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"item": link, "views": views})
}

// POST /buyer-links/:id/revoke {reason}
func (h *QC) RevokeBuyerLink(c *gin.Context) {
	var in struct {
		Reason string `json:"reason"`
	}
	_ = c.ShouldBindJSON(&in)
	link, err := h.Store.RevokeBuyerLink(c.Request.Context(), c.Param("id"), actorOf(c), in.Reason)
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not revoke the link"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"item": link})
}

// coarseNetwork keeps the /24 (IPv4) or /48 (IPv6) of an address: enough to
// tell one buyer's office from another, not to identify a person.
func coarseNetwork(addr string) string {
	ip := net.ParseIP(strings.TrimSpace(addr))
	if ip == nil {
		return ""
	}
	if v4 := ip.To4(); v4 != nil {
		return (&net.IPNet{IP: v4.Mask(net.CIDRMask(24, 32)), Mask: net.CIDRMask(24, 32)}).String()
	}
	return (&net.IPNet{IP: ip.Mask(net.CIDRMask(48, 128)), Mask: net.CIDRMask(48, 128)}).String()
}

// publicLink resolves the token on a public request and answers for it when
// it does not open a shareable certificate. ok is false when it answered.
func (h *QC) publicLink(c *gin.Context, what string) (store.BuyerLink, pdf.CoAData, bool) {
	c.Header("Cache-Control", "no-store")
	c.Header("X-Robots-Tag", "noindex, nofollow")
	c.Header("Referrer-Policy", "no-referrer")
	ctx := c.Request.Context()
	link, err := h.Store.ResolveBuyerLink(ctx, c.Param("token"))
	switch {
	case errors.Is(err, store.ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "link_not_found"})
		return link, pdf.CoAData{}, false
	case errors.Is(err, store.ErrLinkGone):
		c.JSON(http.StatusGone, gin.H{"error": "link_" + link.Status})
		return link, pdf.CoAData{}, false
	case err != nil:
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "unavailable"})
		return link, pdf.CoAData{}, false
	}
	_, payload, err := h.shareableCoA(ctx, link.CoaNumber)
	switch {
	case errors.Is(err, store.ErrNotShareable), errors.Is(err, store.ErrNotFound):
		c.JSON(http.StatusGone, gin.H{"error": "certificate_withdrawn"})
		return link, payload, false
	case err != nil:
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "unavailable"})
		return link, payload, false
	}
	_ = h.Store.RecordBuyerLinkView(ctx, link.BusinessID, what, coarseNetwork(c.ClientIP()), c.Request.UserAgent())
	return link, payload, true
}

// GET /public/coa/:token — the certificate as its template prints it.
func (h *QC) PublicCoA(c *gin.Context) {
	link, p, ok := h.publicLink(c, "page")
	if !ok {
		return
	}
	t := p.Template
	rows := make([]gin.H, 0, len(t.Rows))
	for _, r := range t.Rows {
		rows = append(rows, gin.H{"label": r.Label, "result": r.Result, "unit": r.Unit, "spec": r.Spec, "verdict": r.Verdict})
	}
	sigs := []gin.H{}
	if t.ShowSignatures {
		for _, s := range p.Signatures {
			sigs = append(sigs, gin.H{"name": s.Name, "meaning": s.Meaning, "signed_at": s.SignedAt})
		}
	}
	c.JSON(http.StatusOK, gin.H{"certificate": gin.H{
		"coa_number":       p.CoaNumber,
		"lot":              p.LotBusinessID,
		"issued_at":        p.IssuedAt,
		"title":            t.Title,
		"customer":         t.Customer,
		"header_note":      t.HeaderNote,
		"footer_statement": t.FooterStatement,
		"rows":             rows,
		"signatures":       sigs,
		"recipient":        link.RecipientName,
		"expires_at":       link.ExpiresAt,
	}})
}

// GET /public/coa/:token/pdf — the same certificate as a PDF.
func (h *QC) PublicCoAPDF(c *gin.Context) {
	_, p, ok := h.publicLink(c, "pdf")
	if !ok {
		return
	}
	// The batch and internal document reference are QC's filing, not the
	// buyer's; the rest is the certificate as signed.
	p.BatchBusinessID, p.DocumentRef = "", ""
	raw, err := pdf.RenderCoA(p)
	writePDFResponse(c, safeFilename(p.CoaNumber)+".pdf", raw, err)
}
