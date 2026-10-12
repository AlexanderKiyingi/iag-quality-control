package handlers

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"iag-quality-control/backend/internal/esign"
	"iag-quality-control/backend/internal/middleware"
	"iag-quality-control/backend/internal/pdf"
	"iag-quality-control/backend/internal/store"
)

/*
Electronic signatures (migration 019, 21 CFR Part 11 / EU Annex 11).

A signing request carries two credentials: the session (Authorization) and a
signature token (X-Signature-Token) that iag-authentication issues only after
the person re-enters their password. The token must belong to the person
making the request, be fresh, and be unused.
*/

// signerFrom verifies the signature token on a request.
func (h *QC) signerFrom(c *gin.Context) (esign.Signer, error) {
	callerSub := ""
	if claims, ok := middleware.PlatformClaims(c); ok && claims != nil {
		callerSub = claims.Subject
	}
	signer, err := esign.Verify(h.SignatureVerifier, c.GetHeader(esign.Header), callerSub, time.Now())
	if err != nil {
		return esign.Signer{}, err
	}
	used, err := h.Store.SignatureTokenUsed(c.Request.Context(), signer.JTI)
	if err != nil {
		return esign.Signer{}, err
	}
	if used {
		return esign.Signer{}, errSignatureReused
	}
	return signer, nil
}

// signPermissions lists the registers signed through POST /signatures and
// the permission each needs. Specs, CoAs and certification stages carry their
// signature on their own write routes instead.
var signPermissions = map[string]string{
	"release-decisions": "qc.decide_release",
	"non-conformances":  "qc.change_compliance",
	"capas":             "qc.change_compliance",
}

var errSignatureReused = errors.New("that signature has already been used — re-enter your password to sign again")

// respondSignatureErr answers a failed signature check: 403 when the token is
// missing, stale, someone else's or replayed — never 401, which a client would
// read as a lost session.
func respondSignatureErr(c *gin.Context, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, esign.ErrMissing), errors.Is(err, esign.ErrStale), errors.Is(err, esign.ErrNotYours),
		errors.Is(err, esign.ErrNotForSign), errors.Is(err, esign.ErrInvalid), errors.Is(err, errSignatureReused):
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error(), "code": "signature_required"})
		return true
	default:
		return false
	}
}

// SignRecord records a signature on a record and applies what it stands for:
// approving a release decision, closing an NC or a CAPA.
// POST /api/v1/signatures {entity, business_id, meaning, reason}
func (h *QC) SignRecord(c *gin.Context) {
	var body struct {
		Entity     string `json:"entity" binding:"required"`
		BusinessID string `json:"business_id" binding:"required"`
		Meaning    string `json:"meaning" binding:"required"`
		Reason     string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// Signing a record needs the permission that governs that register — the
	// same one a plain edit to it needs, or the stronger one for releases.
	perm, ok := signPermissions[strings.TrimSpace(body.Entity)]
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "that register is not signed through this route"})
		return
	}
	if middleware.RequirePermission(perm)(c); c.IsAborted() {
		return
	}
	signer, err := h.signerFrom(c)
	if respondSignatureErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not check the signature"})
		return
	}
	sig, err := h.Store.SignRecord(c.Request.Context(), store.SignInput{
		Entity: body.Entity, BusinessID: body.BusinessID, Meaning: body.Meaning, Reason: body.Reason, Signer: signer,
	})
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not record the signature"})
		return
	}
	resp := gin.H{"signature": sig}
	// A release only takes effect once it is signed: that is when the batch
	// is announced as released to warehouse and traceability.
	if sig.Entity == "release-decisions" && sig.Meaning == "approved" {
		if ev := h.announceSignedRelease(c.Request.Context(), sig.BusinessID); ev != "" {
			resp["event_type"] = ev
		}
	}
	c.JSON(http.StatusOK, resp)
}

func (h *QC) announceSignedRelease(ctx context.Context, businessID string) string {
	items, err := h.Store.ListReleaseDecisions(ctx, "", "", 500)
	if err != nil {
		return ""
	}
	for _, item := range items {
		if item.BusinessID != businessID {
			continue
		}
		eventType := store.ReleaseEventType(item.Decision)
		if eventType == "" {
			return ""
		}
		data := map[string]any{
			"batch_business_id":   item.BatchRef,
			"release_business_id": item.BusinessID,
			"product":             item.Product,
			"decision":            item.Decision,
			"decided_by":          item.DecidedBy,
			"check_refs":          item.CheckRefs,
			"signed":              true,
		}
		if err := h.publish(ctx, eventType, data); err != nil {
			log.Printf("quality-control: release %s signed but %s not published: %v", businessID, eventType, err)
			return ""
		}
		return eventType
	}
	return ""
}

// ListSignatures lists a record's signatures, each with whether the record
// still matches what was signed.
// GET /api/v1/signatures?entity=&id=
func (h *QC) ListSignatures(c *gin.Context) {
	items, err := h.Store.ListSignatures(c.Request.Context(), c.Query("entity"), c.Query("id"), 0)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list signatures"})
		return
	}
	type row struct {
		store.Signature
		RecordUnchanged bool `json:"record_unchanged"`
	}
	out := make([]row, 0, len(items))
	for _, sig := range items {
		ok, err := h.Store.VerifySignature(c.Request.Context(), sig)
		if err != nil {
			ok = false
		}
		out = append(out, row{Signature: sig, RecordUnchanged: ok})
	}
	c.JSON(http.StatusOK, gin.H{"items": out, "total": len(out)})
}

// coaSignatureLines collects the signatures behind a certificate: its own, and
// those on the certification request that issued it.
func (h *QC) coaSignatureLines(ctx context.Context, coaNumber string) []pdf.SignatureLine {
	var lines []pdf.SignatureLine
	add := func(entity, id, context string) {
		sigs, err := h.Store.ListSignatures(ctx, entity, id, 0)
		if err != nil {
			return
		}
		for _, s := range sigs {
			lines = append(lines, pdf.SignatureLine{
				Name: s.SignerName, Email: s.SignerEmail, Meaning: strings.ToUpper(s.Meaning[:1]) + s.Meaning[1:],
				SignedAt: s.SignedAt.UTC().Format("2006-01-02 15:04:05 UTC"), Reason: s.Reason, Context: context,
			})
		}
	}
	if req, err := h.Store.CertificationRequestForCoA(ctx, coaNumber); err == nil && req != "" {
		add("certification-requests", req, "certification "+req)
	}
	add("coa", coaNumber, "")
	return lines
}
