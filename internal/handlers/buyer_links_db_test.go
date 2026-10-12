package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"iag-quality-control/backend/internal/db"
	"iag-quality-control/backend/internal/esign"
	"iag-quality-control/backend/internal/migrate"
	"iag-quality-control/backend/internal/store"
)

// Buyer links (027) end to end against a real Postgres: who can be sent a
// certificate, what they see, and that revoking or withdrawing ends it at once.
func TestBuyerLinksAgainstRealPostgres(t *testing.T) {
	dsn := os.Getenv("QC_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("QC_TEST_DATABASE_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool, err := db.NewPool(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := migrate.Up(ctx, pool); err != nil {
		t.Fatal(err)
	}
	st := store.New(pool)
	run := strings.ReplaceAll(time.Now().UTC().Format("150405.000"), ".", "")
	customer, coaNo, batch := "CUST-BL-"+run, "COA-BL-"+run, "BATCH-BL-"+run

	gin.SetMode(gin.TestMode)
	h := &QC{Store: st}
	r := gin.New()
	r.POST("/coa/:coaNumber/buyer-links", h.CreateBuyerLink)
	r.GET("/coa/:coaNumber/buyer-links", h.ListBuyerLinks)
	r.GET("/buyer-links/:id/views", h.ListBuyerLinkViews)
	r.POST("/buyer-links/:id/revoke", h.RevokeBuyerLink)
	r.GET("/public/coa/:token", h.PublicCoA)
	r.GET("/public/coa/:token/pdf", h.PublicCoAPDF)
	call := func(method, path, body string) (int, map[string]any, http.Header) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "203.0.113.77:5000"
		r.ServeHTTP(w, req)
		out := map[string]any{}
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out, w.Header()
	}
	sign := func(entity, id, meaning string) {
		t.Helper()
		if _, err := st.SignRecord(ctx, store.SignInput{Entity: entity, BusinessID: id, Meaning: meaning, Reason: "ok",
			Signer: esign.Signer{Subject: "u-qa", Email: "qa@iag", Name: "Quinn QA", AuthTime: time.Now(), JTI: "bl-" + entity + run}}); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := pool.Exec(ctx, `INSERT INTO qc_samples (business_id, batch_business_id) VALUES ($1, $2)`, "S-BL-"+run, batch); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO qc_physical_tests (business_id, sample_business_id, batch_business_id, moisture_pct, tested_at)
		VALUES ($1, $2, $3, 10.4, NOW())`, "PT-BL-"+run, "S-BL-"+run, batch); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateCoA(ctx, store.CreateCoAInput{CoaNumber: coaNo, LotBusinessID: "LOT-BL-" + run,
		BatchBusinessID: batch, IssuedBy: "qa@iag", CustomerRef: customer}); err != nil {
		t.Fatal(err)
	}

	t.Run("an unsigned certificate cannot be shared", func(t *testing.T) {
		code, body, _ := call("POST", "/coa/"+coaNo+"/buyer-links", `{"recipient_email":"buyer@roaster.example"}`)
		if code != http.StatusConflict || body["code"] != "not_shareable" {
			t.Fatalf("%d %v", code, body)
		}
	})
	sign("coa", coaNo, "issued")

	t.Run("nor one without a template", func(t *testing.T) {
		if _, ok, _ := st.ResolveCoATemplate(ctx, "", customer); ok {
			t.Skip("a house default template exists in this database")
		}
		code, body, _ := call("POST", "/coa/"+coaNo+"/buyer-links", `{"recipient_email":"buyer@roaster.example"}`)
		if code != http.StatusConflict || !strings.Contains(body["error"].(string), "template") {
			t.Fatalf("%d %v", code, body)
		}
	})
	if _, err := st.PutCoATemplate(ctx, store.CoATemplateInput{Name: "Buyer " + run, CustomerRef: customer, Title: "Certificate of Analysis",
		Parameters: []store.CoAField{{Parameter: "moisture", Label: "Moisture", Unit: "%"}}, Actor: "qa@iag"}); err != nil {
		t.Fatal(err)
	}

	var token, id string
	t.Run("QA makes a link; the token is shown once and only its hash is kept", func(t *testing.T) {
		if code, _, _ := call("POST", "/coa/"+coaNo+"/buyer-links", `{}`); code != http.StatusBadRequest {
			t.Fatalf("a link for nobody: %d", code)
		}
		code, body, _ := call("POST", "/coa/"+coaNo+"/buyer-links", `{"recipient_name":"Rae Buyer","recipient_email":"Buyer@Roaster.example","expires_in_days":900}`)
		if code != http.StatusCreated {
			t.Fatalf("%d %v", code, body)
		}
		item := body["item"].(map[string]any)
		token, id = item["token"].(string), item["business_id"].(string)
		exp, _ := time.Parse(time.RFC3339Nano, item["expires_at"].(string))
		if len(token) < 40 || item["recipient_email"] != "buyer@roaster.example" || item["customer_ref"] != customer ||
			exp.After(time.Now().AddDate(0, 0, 366)) || item["status"] != "active" {
			t.Fatalf("link: %v", item)
		}
		var stored string
		if err := pool.QueryRow(ctx, `SELECT token_hash FROM qc_buyer_links WHERE business_id = $1`, id).Scan(&stored); err != nil ||
			stored != store.HashLinkToken(token) || strings.Contains(stored, token) {
			t.Fatalf("stored %q %v", stored, err)
		}
		_, list, _ := call("GET", "/coa/"+coaNo+"/buyer-links", "")
		items := list["items"].([]any)
		if len(items) != 1 || items[0].(map[string]any)["token"] != nil {
			t.Fatalf("a listing never carries the token: %v", items)
		}
	})

	t.Run("the buyer sees the template's results and nothing internal", func(t *testing.T) {
		code, body, hdr := call("GET", "/public/coa/"+token, "")
		if code != http.StatusOK || hdr.Get("Cache-Control") != "no-store" {
			t.Fatalf("%d %v %v", code, body, hdr)
		}
		cert := body["certificate"].(map[string]any)
		rows := cert["rows"].([]any)
		if len(rows) != 1 || rows[0].(map[string]any)["result"] != "10.4" || cert["recipient"] != "Rae Buyer" {
			t.Fatalf("certificate: %v", cert)
		}
		raw, _ := json.Marshal(body)
		for _, internal := range []string{batch, "qa@iag", "cup_score", "token"} {
			if strings.Contains(string(raw), internal) {
				t.Fatalf("the public view leaks %q: %s", internal, raw)
			}
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/public/coa/"+token+"/pdf", nil))
		if w.Code != http.StatusOK || !strings.HasPrefix(w.Body.String(), "%PDF") || strings.Contains(w.Body.String(), batch) {
			t.Fatalf("pdf: %d", w.Code)
		}
		_, views, _ := call("GET", "/buyer-links/"+id+"/views", "")
		vs := views["views"].([]any)
		if len(vs) != 2 || vs[1].(map[string]any)["network"] != "203.0.113.0/24" || views["item"].(map[string]any)["views"].(float64) != 2 {
			t.Fatalf("views: %v", views)
		}
	})

	t.Run("an unknown token is not found", func(t *testing.T) {
		if code, body, _ := call("GET", "/public/coa/"+token+"x", ""); code != http.StatusNotFound {
			t.Fatalf("%d %v", code, body)
		}
	})

	t.Run("a withdrawn certificate ends its links", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `UPDATE qc_coa SET status = 'withdrawn' WHERE coa_number = $1`, coaNo); err != nil {
			t.Fatal(err)
		}
		code, body, _ := call("GET", "/public/coa/"+token, "")
		if code != http.StatusGone || body["error"] != "certificate_withdrawn" {
			t.Fatalf("%d %v", code, body)
		}
		if _, err := pool.Exec(ctx, `UPDATE qc_coa SET status = 'active' WHERE coa_number = $1`, coaNo); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("revoking needs a reason, takes effect at once, and happens once", func(t *testing.T) {
		if code, _, _ := call("POST", "/buyer-links/"+id+"/revoke", `{}`); code != http.StatusUnprocessableEntity {
			t.Fatalf("no reason: %d", code)
		}
		code, body, _ := call("POST", "/buyer-links/"+id+"/revoke", `{"reason":"sent to the wrong buyer"}`)
		if code != http.StatusOK || body["item"].(map[string]any)["status"] != "revoked" {
			t.Fatalf("%d %v", code, body)
		}
		if code, body, _ := call("GET", "/public/coa/"+token+"/pdf", ""); code != http.StatusGone || body["error"] != "link_revoked" {
			t.Fatalf("after revoke: %d %v", code, body)
		}
		if code, _, _ := call("POST", "/buyer-links/"+id+"/revoke", `{"reason":"again"}`); code != http.StatusConflict {
			t.Fatalf("revoked twice: %d", code)
		}
		var logged int
		_ = pool.QueryRow(ctx, `SELECT COUNT(*) FROM qc_change_log WHERE table_name = 'qc_buyer_links' AND business_id = $1`, id).Scan(&logged)
		if logged != 2 {
			t.Fatalf("change log has creation and revocation, not views: %d", logged)
		}
	})

	t.Run("an expired link is gone", func(t *testing.T) {
		code, body, _ := call("POST", "/coa/"+coaNo+"/buyer-links", `{"recipient_name":"Short"}`)
		if code != http.StatusCreated {
			t.Fatalf("%d %v", code, body)
		}
		item := body["item"].(map[string]any)
		if _, err := pool.Exec(ctx, `UPDATE qc_buyer_links SET expires_at = NOW() - INTERVAL '1 minute' WHERE business_id = $1`, item["business_id"]); err != nil {
			t.Fatal(err)
		}
		if code, body, _ := call("GET", "/public/coa/"+item["token"].(string), ""); code != http.StatusGone || body["error"] != "link_expired" {
			t.Fatalf("%d %v", code, body)
		}
	})
}

func TestCoarseNetwork(t *testing.T) {
	for in, want := range map[string]string{
		"203.0.113.77":        "203.0.113.0/24",
		"2001:db8:abcd:12::1": "2001:db8:abcd::/48",
		"not an address":      "",
		"::ffff:198.51.100.9": "198.51.100.0/24",
	} {
		if got := coarseNetwork(in); got != want {
			t.Errorf("%s: %s, want %s", in, got, want)
		}
	}
}
