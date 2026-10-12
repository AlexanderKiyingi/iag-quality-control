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

	"iag-quality-control/backend/internal/clients"
	"iag-quality-control/backend/internal/config"
	"iag-quality-control/backend/internal/db"
	"iag-quality-control/backend/internal/esign"
	"iag-quality-control/backend/internal/migrate"
	"iag-quality-control/backend/internal/store"
)

// Qualification to run a method, against a real Postgres and a fake HR.
// Env-gated like the store's database tests.
func TestQualificationAgainstRealPostgres(t *testing.T) {
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
	method, sop := "LM-QUAL-"+run, "SOP-QUAL-"+run
	str := func(v string) *string { return &v }

	// An SOP in force, read by ana but not by ben.
	if _, err := st.CreateDocument(ctx, store.DocumentInput{BusinessID: sop, Title: str("Oven moisture"), AttachmentID: "ATT-1", Actor: "qa@iag"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SubmitForReview(ctx, sop, 1, "qa@iag"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SignRecord(ctx, store.SignInput{Entity: "document-versions", BusinessID: sop + "-v1", Meaning: "approved",
		Reason: "ok", Signer: esign.Signer{Subject: "u", Email: "qa@iag", AuthTime: time.Now(), JTI: "q" + run}}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.MakeEffective(ctx, sop, 1, "", "qa@iag"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Acknowledge(ctx, sop, 1, "u-a", "ana@iag", "Ana"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertLabMethod(ctx, store.UpsertLabMethodInput{BusinessID: method, Name: "Oven moisture", SopRef: str(sop)}); err != nil {
		t.Fatal(err)
	}

	// HR: ana and ben trained, cy's training lapsed.
	past := time.Now().AddDate(0, -1, 0)
	future := time.Now().AddDate(1, 0, 0)
	hrUp := true
	hr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !hrUp {
			http.Error(w, "down", http.StatusBadGateway)
			return
		}
		all := []clients.Qualification{
			{Email: "ana@iag", EmployeeName: "Ana", Method: method, CourseCode: "C1", ExpiresOn: &future},
			{Email: "ben@iag", EmployeeName: "Ben", Method: method, CourseCode: "C1", ExpiresOn: &future},
			{Email: "cy@iag", EmployeeName: "Cy", Method: method, CourseCode: "C1", ExpiresOn: &past, Expired: true},
		}
		out := []clients.Qualification{}
		for _, q := range all {
			if e := r.URL.Query().Get("email"); e != "" && !strings.EqualFold(e, q.Email) {
				continue
			}
			out = append(out, q)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"items": out})
	}))
	defer hr.Close()

	h := &QC{Cfg: config.Config{QualificationEnforcement: "warn"}, Store: st, ERP: clients.NewERP(hr.URL, "", "", "")}
	cases := []struct {
		email     string
		qualified bool
		reason    string
	}{
		{"ana@iag", true, ""},
		{"ben@iag", false, "has not read " + sop + " v1"},
		{"cy@iag", false, "expired"},
		{"dee@iag", false, "no completed training"},
	}
	for _, c := range cases {
		got := h.checkQualification(ctx, c.email, method)
		if !got.Checked || got.Qualified != c.qualified || (c.reason != "" && !strings.Contains(strings.Join(got.Reasons, "; "), c.reason)) {
			t.Errorf("%s: %+v", c.email, got)
		}
	}
	if got := h.checkQualification(ctx, "ana@iag", "LM-NOT-REGISTERED"); got.Checked {
		t.Errorf("an unregistered method is not checked: %+v", got)
	}

	hrUp = false
	if got := h.checkQualification(ctx, "dee@iag", method); !got.Checked || got.Note == "" {
		t.Errorf("HR down: training unchecked, not failed: %+v", got)
	}
	hrUp = true

	// The matrix.
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/qualification-matrix", nil)
	h.QualificationMatrix(c)
	var body struct {
		People []struct {
			Email string                       `json:"email"`
			Cells map[string]map[string]string `json:"cells"`
		} `json:"people"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"ana@iag": "qualified", "ben@iag": "sop_unread", "cy@iag": "expired"}
	for _, p := range body.People {
		if exp, ok := want[p.Email]; ok && p.Cells[method]["status"] != exp {
			t.Errorf("%s on %s: %v, want %s", p.Email, method, p.Cells[method], exp)
		}
	}
}
