package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"iag-quality-control/backend/internal/middleware"
	"iag-quality-control/backend/internal/store"
)

func (h *QC) ListDocuments(c *gin.Context) {
	items, err := h.Store.ListDocuments(c.Request.Context(), c.Query("type"), c.Query("filter"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list documents"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

func (h *QC) GetDocument(c *gin.Context) {
	item, err := h.Store.GetDocument(c.Request.Context(), c.Param("id"))
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load document"})
		return
	}
	c.JSON(http.StatusOK, item)
}

func bindDocument(c *gin.Context) (store.DocumentInput, bool) {
	var body store.DocumentInput
	if err := bindJSONCoerced(c, &body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return body, false
	}
	body.Actor = actorOf(c)
	return body, true
}

func (h *QC) docResponse(c *gin.Context, item any, err error, status int, what string) {
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not " + what})
		return
	}
	c.JSON(status, item)
}

func (h *QC) CreateDocument(c *gin.Context) {
	body, ok := bindDocument(c)
	if !ok {
		return
	}
	item, err := h.Store.CreateDocument(c.Request.Context(), body)
	h.docResponse(c, item, err, http.StatusCreated, "create document")
}

func (h *QC) UpdateDocument(c *gin.Context) {
	body, ok := bindDocument(c)
	if !ok {
		return
	}
	item, err := h.Store.UpdateDocument(c.Request.Context(), c.Param("id"), body)
	h.docResponse(c, item, err, http.StatusOK, "update document")
}

func (h *QC) NewDocumentVersion(c *gin.Context) {
	body, ok := bindDocument(c)
	if !ok {
		return
	}
	item, err := h.Store.NewDocumentVersion(c.Request.Context(), c.Param("id"), body)
	h.docResponse(c, item, err, http.StatusCreated, "open a new version")
}

func versionParam(c *gin.Context) (int, bool) {
	v, err := strconv.Atoi(c.Param("v"))
	if err != nil || v < 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "the version is a number"})
		return 0, false
	}
	return v, true
}

func (h *QC) UpdateDraft(c *gin.Context) {
	v, ok := versionParam(c)
	if !ok {
		return
	}
	body, ok := bindDocument(c)
	if !ok {
		return
	}
	item, err := h.Store.UpdateDraft(c.Request.Context(), c.Param("id"), v, body)
	h.docResponse(c, item, err, http.StatusOK, "update draft")
}

func (h *QC) SubmitForReview(c *gin.Context) {
	v, ok := versionParam(c)
	if !ok {
		return
	}
	item, err := h.Store.SubmitForReview(c.Request.Context(), c.Param("id"), v, actorOf(c))
	h.docResponse(c, item, err, http.StatusOK, "submit for review")
}

// MakeEffective puts an approved version in force and announces it, so
// training (HR) knows who now has a new version to read.
func (h *QC) MakeEffective(c *gin.Context) {
	v, ok := versionParam(c)
	if !ok {
		return
	}
	var body struct {
		EffectiveDate string `json:"effective_date"`
	}
	_ = c.ShouldBindJSON(&body)
	item, err := h.Store.MakeEffective(c.Request.Context(), c.Param("id"), v, body.EffectiveDate, actorOf(c))
	if err == nil {
		doc, _ := h.Store.GetDocument(c.Request.Context(), item.DocBusinessID)
		_ = h.publish(c.Request.Context(), "qc.document.effective", map[string]any{
			"document": item.DocBusinessID, "version": item.Version, "title": doc.Title, "doc_type": doc.DocType,
			"effective_date": item.EffectiveDate, "next_review_date": item.NextReviewDate,
		})
	}
	h.docResponse(c, item, err, http.StatusOK, "make the version effective")
}

func (h *QC) ObsoleteDocument(c *gin.Context) {
	var body struct {
		Reason string `json:"reason"`
	}
	_ = c.ShouldBindJSON(&body)
	item, err := h.Store.ObsoleteDocument(c.Request.Context(), c.Param("id"), body.Reason, actorOf(c))
	h.docResponse(c, item, err, http.StatusOK, "withdraw document")
}

// AcknowledgeDocument records the signed-in person's "read and understood" of
// the effective version — the identity comes from the verified token only.
func (h *QC) AcknowledgeDocument(c *gin.Context) {
	v, ok := versionParam(c)
	if !ok {
		return
	}
	claims, ok := middleware.PlatformClaims(c)
	if !ok || claims == nil || strings.TrimSpace(claims.Email) == "" {
		c.JSON(http.StatusForbidden, gin.H{"error": "acknowledging needs a signed-in person"})
		return
	}
	item, err := h.Store.Acknowledge(c.Request.Context(), c.Param("id"), v, claims.Subject, claims.Email, claims.Name)
	if err == nil {
		_ = h.publish(c.Request.Context(), "qc.document.acknowledged", map[string]any{
			"document": item.DocBusinessID, "version": item.Version, "user_email": item.UserEmail,
			"user_sub": item.UserSub, "acknowledged_at": item.AcknowledgedAt,
		})
	}
	h.docResponse(c, item, err, http.StatusOK, "record acknowledgement")
}

func (h *QC) ListAcknowledgements(c *gin.Context) {
	items, err := h.Store.ListAcknowledgements(c.Request.Context(), c.Query("document"), c.Query("email"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list acknowledgements"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}
