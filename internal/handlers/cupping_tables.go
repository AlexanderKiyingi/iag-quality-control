package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"iag-quality-control/backend/internal/middleware"
	"iag-quality-control/backend/internal/store"
)

/*
Cupping tables (migration 020): a sitting where a panel cups several samples,
usually blind. A cupper (qc.record_tests) sees cup codes and scores them; who
may see behind the codes before the table closes — and close it, which reveals
them to everyone — is qc.change_sample, the lab manager.
*/
const revealPermission = "qc.change_sample"

func mayReveal(c *gin.Context) bool {
	claims, ok := middleware.PlatformClaims(c)
	if !ok || claims == nil {
		// No platform token: the service-token / dev path, which every other
		// gate in this service also lets through.
		return true
	}
	return claims.IsSuperuser || claims.IsStaff || claims.HasPermission(revealPermission)
}

func (h *QC) ListCuppingTables(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "200"))
	items, err := h.Store.ListCuppingTables(c.Request.Context(), c.Query("status"), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list cupping tables"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

func (h *QC) CreateCuppingTable(c *gin.Context) {
	var body struct {
		Title              string   `json:"title"`
		ScheduledOn        string   `json:"scheduled_on"`
		Form               string   `json:"form"`
		Blind              *bool    `json:"blind"`
		Calibration        bool     `json:"calibration"`
		ReferenceEvaluator string   `json:"reference_evaluator"`
		CupsPerSample      int      `json:"cups_per_sample"`
		SampleIDs          []string `json:"sample_business_ids"`
		Notes              string   `json:"notes"`
	}
	if err := bindJSONCoerced(c, &body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	table, err := h.Store.CreateCuppingTable(c.Request.Context(), store.CreateCuppingTableInput{
		Title: body.Title, ScheduledOn: body.ScheduledOn, Form: body.Form, Blind: body.Blind,
		Calibration: body.Calibration, ReferenceEvaluator: body.ReferenceEvaluator,
		CupsPerSample: body.CupsPerSample, SampleIDs: body.SampleIDs, Notes: body.Notes,
		Actor: actorOf(c),
	})
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create cupping table"})
		return
	}
	// Whoever set the table up chose the samples; the response names them only
	// if they may see behind the codes.
	if table.Blind && !mayReveal(c) {
		table, err = h.Store.GetCuppingTable(c.Request.Context(), table.BusinessID, false)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load cupping table"})
			return
		}
	}
	c.JSON(http.StatusCreated, table)
}

// GetCuppingTable serves a table; ?reveal=true unmasks an open blind table for
// someone who may.
func (h *QC) GetCuppingTable(c *gin.Context) {
	reveal := c.Query("reveal") == "true"
	if reveal && !mayReveal(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "permission denied: " + revealPermission,
			"required_permission": revealPermission})
		return
	}
	table, err := h.Store.GetCuppingTable(c.Request.Context(), c.Param("id"), reveal)
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load cupping table"})
		return
	}
	c.JSON(http.StatusOK, table)
}

func (h *QC) CloseCuppingTable(c *gin.Context) {
	var body struct {
		Reason string `json:"reason"`
	}
	_ = c.ShouldBindJSON(&body)
	table, err := h.Store.CloseCuppingTable(c.Request.Context(), c.Param("id"), actorOf(c), body.Reason)
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not close cupping table"})
		return
	}
	c.JSON(http.StatusOK, table)
}

/*
SaveTableCupScore records one cupper's sheet for a cup on a table, addressed
by cup code. A tablet re-sending a sheet it scored offline lands on the same
(cup, cupper) row and returns 200 again; a client_ref reused on a different
sheet is a 409.
*/
func (h *QC) SaveTableCupScore(c *gin.Context) {
	var body store.CuppingScoreInput
	if err := bindJSONCoerced(c, &body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	sessionID, err := h.Store.SessionForCup(c.Request.Context(), c.Param("id"), c.Param("code"))
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not find that cup"})
		return
	}
	item, err := h.Store.SaveCuppingScore(c.Request.Context(), sessionID, body)
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not save cupping sheet"})
		return
	}
	// The cupper gets their own sheet back — not the session id, which the
	// session list would turn into a lot.
	item.SessionBusinessID = ""
	c.JSON(http.StatusOK, gin.H{"cup_code": c.Param("code"), "sheet": item})
}

// CuppingTableResults serves each cup's panel — sheets and agreement (or, on a
// calibration table, each cupper against the reference). Only once the codes
// may be seen.
func (h *QC) CuppingTableResults(c *gin.Context) {
	ctx := c.Request.Context()
	table, err := h.Store.GetCuppingTable(ctx, c.Param("id"), mayReveal(c))
	if respondStoreErr(c, err) {
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load cupping table"})
		return
	}
	if !table.Revealed {
		c.JSON(http.StatusForbidden, gin.H{"error": "results show once the table is closed",
			"code": "table_blind"})
		return
	}
	threshold, _ := strconv.ParseFloat(c.Query("threshold"), 64)
	type cupResult struct {
		store.TableCup
		Panel store.CuppingPanel `json:"panel"`
	}
	out := make([]cupResult, 0, len(table.Cups))
	for _, cup := range table.Cups {
		panel, err := h.Store.GetCuppingPanel(ctx, cup.SessionBusinessID, threshold)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load cupping panel"})
			return
		}
		out = append(out, cupResult{TableCup: cup, Panel: panel})
	}
	c.JSON(http.StatusOK, gin.H{"table": table.CuppingTable, "cups": out})
}
