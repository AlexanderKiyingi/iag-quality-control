package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

func (h *QC) scmUnavailable(c *gin.Context) bool {
	if h.SCM == nil || !h.SCM.Enabled() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "supply-chain integration disabled"})
		return true
	}
	return false
}

func (h *QC) scmJSON(c *gin.Context, data json.RawMessage, meta any) {
	resp := gin.H{}
	if len(data) > 0 {
		resp["data"] = json.RawMessage(data)
	} else {
		resp["data"] = []any{}
	}
	if meta != nil {
		resp["meta"] = meta
	}
	c.JSON(http.StatusOK, resp)
}

func (h *QC) ListBatches(c *gin.Context) {
	if h.scmUnavailable(c) {
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "0"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "0"))
	data, meta, err := h.SCM.ListBatches(c.Request.Context(), c.Query("stage"), c.Query("bean"), page, limit)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "scm request failed", "detail": err.Error()})
		return
	}
	h.scmJSON(c, data, meta)
}

func (h *QC) GetBatch(c *gin.Context) {
	if h.scmUnavailable(c) {
		return
	}
	data, err := h.SCM.GetBatch(c.Request.Context(), c.Param("businessId"))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "scm request failed", "detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": json.RawMessage(data)})
}

func (h *QC) GetBatchPipeline(c *gin.Context) {
	batchID := c.Param("businessId")
	resp := gin.H{"batch_business_id": batchID}

	if h.SCM != nil && h.SCM.Enabled() {
		if data, err := h.SCM.GetBatch(c.Request.Context(), batchID); err == nil {
			resp["batch"] = json.RawMessage(data)
		}
	}

	if summary, err := h.Store.GetBatchLabSummary(c.Request.Context(), batchID); err == nil {
		resp["lab_summary"] = summary
	}
	samples, _ := h.Store.ListSamples(c.Request.Context(), "all", batchID, 50)
	resp["samples"] = samples
	resp["sample_count"] = len(samples)

	if h.MES != nil && h.MES.Enabled() {
		if mesPipe, err := h.MES.BatchPipeline(c.Request.Context(), batchID); err == nil {
			resp["mes_runs"] = mesPipe.Runs
			if len(mesPipe.CCPReadings) > 0 {
				resp["mes_ccp_readings"] = mesPipe.CCPReadings
			}
		}
	}

	c.JSON(http.StatusOK, resp)
}

func (h *QC) ListExportLots(c *gin.Context) {
	if h.scmUnavailable(c) {
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "0"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "0"))
	data, meta, err := h.SCM.ListExportLots(c.Request.Context(), page, limit)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "scm request failed", "detail": err.Error()})
		return
	}
	h.scmJSON(c, data, meta)
}

func (h *QC) GetExportLot(c *gin.Context) {
	if h.scmUnavailable(c) {
		return
	}
	data, err := h.SCM.GetExportLot(c.Request.Context(), c.Param("businessId"))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "scm request failed", "detail": err.Error()})
		return
	}
	coa, err := h.Store.GetCoAByLot(c.Request.Context(), c.Param("businessId"))
	if err == nil {
		c.JSON(http.StatusOK, gin.H{"data": json.RawMessage(data), "coa": coa})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": json.RawMessage(data)})
}

func (h *QC) ListFarmers(c *gin.Context) {
	if h.scmUnavailable(c) {
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "0"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "0"))
	data, meta, err := h.SCM.ListFarmers(c.Request.Context(), c.Query("search"), c.Query("bean"), c.Query("cert"), page, limit)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "scm request failed", "detail": err.Error()})
		return
	}
	h.scmJSON(c, data, meta)
}

func (h *QC) GetFarmer(c *gin.Context) {
	if h.scmUnavailable(c) {
		return
	}
	data, err := h.SCM.GetFarmer(c.Request.Context(), c.Param("businessId"))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "scm request failed", "detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": json.RawMessage(data)})
}
