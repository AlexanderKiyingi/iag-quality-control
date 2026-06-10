package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alvor-technologies/iag-platform-go/authclient"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func init() { gin.SetMode(gin.TestMode) }

// RequireAuth must reject requests that carry no authenticated principal. This
// guards the regression that left /api/v1 wide open (only a "Bearer " prefix
// check) before the platform cutover.
func TestRequireAuth_RejectsAnonymous(t *testing.T) {
	r := gin.New()
	r.Use((&PlatformAuth{}).RequireAuth())
	r.GET("/x", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous request: got %d, want 401", w.Code)
	}
}

func TestRequireAuth_AllowsPrincipal(t *testing.T) {
	r := gin.New()
	r.Use(func(c *gin.Context) {
		setPrincipal(c, uuid.New(), &authclient.Claims{}, nil)
		c.Next()
	})
	r.Use((&PlatformAuth{}).RequireAuth())
	r.GET("/x", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("authenticated request: got %d, want 200", w.Code)
	}
}

func TestRequireAuth_PublicProbeBypasses(t *testing.T) {
	r := gin.New()
	r.Use((&PlatformAuth{}).RequireAuth())
	r.GET("/health", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("health probe: got %d, want 200", w.Code)
	}
}

// In strict (production) RBAC, a principal lacking the required permission is
// denied; a principal holding it passes.
func TestRequirePermission_StrictRBAC(t *testing.T) {
	cases := []struct {
		name  string
		perms []string
		want  int
	}{
		{"missing permission denied", []string{"qc.view_coa"}, http.StatusForbidden},
		{"present permission allowed", []string{"qc.view_samples"}, http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := gin.New()
			r.Use(func(c *gin.Context) {
				setPrincipal(c, uuid.New(), &authclient.Claims{Permissions: tc.perms}, tc.perms)
				c.Next()
			})
			r.Use(StrictRBAC())
			r.GET("/x", RequirePermission("qc.view_samples"), func(c *gin.Context) { c.Status(http.StatusOK) })

			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
			if w.Code != tc.want {
				t.Fatalf("got %d, want %d", w.Code, tc.want)
			}
		})
	}
}

// A superuser bypasses per-permission checks even under strict RBAC.
func TestRequirePermission_SuperuserBypass(t *testing.T) {
	r := gin.New()
	r.Use(func(c *gin.Context) {
		setPrincipal(c, uuid.New(), &authclient.Claims{IsSuperuser: true}, nil)
		c.Next()
	})
	r.Use(StrictRBAC())
	r.GET("/x", RequirePermission("qc.issue_coa"), func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("superuser: got %d, want 200", w.Code)
	}
}
