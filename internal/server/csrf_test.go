package server

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
)

// TestCSRFTokenMatchesCookieOnFirstLoad guards against the double-mint bug where
// a handler and renderTemplate each call Token(), causing the rendered token to
// differ from the final cookie on the first request (no valid cookie yet / after
// a restart) — which 403s the first POST (login, delete feed, save settings…).
func TestCSRFTokenMatchesCookieOnFirstLoad(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close(t)

	// An admin must exist so /admin/login renders the form instead of redirecting
	// to first-run /setup.
	if err := ts.authService.CreateUser(ts.db.DB, "admin", "RegressTest!2026"); err != nil {
		t.Fatalf("create admin: %v", err)
	}

	handler := ts.server.Routes()
	req := httptest.NewRequest(http.MethodGet, "/admin/login", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("GET /admin/login: expected 200, got %d", rr.Code)
	}

	m := regexp.MustCompile(`name="csrf_token" value="([^"]+)"`).FindStringSubmatch(rr.Body.String())
	if m == nil {
		t.Fatalf("no csrf_token field found in login page")
	}
	rendered := m[1]

	var cookieToken string
	count := 0
	for _, c := range rr.Result().Cookies() {
		if c.Name == "csrf_token" {
			cookieToken = c.Value
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly one csrf_token cookie, got %d", count)
	}
	if rendered != cookieToken {
		t.Errorf("rendered CSRF token %q != cookie %q — first POST would 403", rendered, cookieToken)
	}
}
