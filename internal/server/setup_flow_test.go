package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLoginRedirectsToSetupOnFirstRun(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close(t)

	handler := ts.server.Routes()
	req := httptest.NewRequest(http.MethodGet, "/admin/login", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected %d, got %d", http.StatusSeeOther, rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/setup" {
		t.Fatalf("expected redirect to /setup, got %q", loc)
	}
}

func TestSetupPostCreatesAdminAndHidesSetup(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close(t)

	handler := ts.server.Routes()

	body, err := json.Marshal(map[string]string{
		"siteTitle":       "Onboarding Title",
		"username":        "AdminUser",
		"password":        "Str0ng!Passw0rd123",
		"confirmPassword": "Str0ng!Passw0rd123",
	})
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/setup", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	addCSRFToken(t, ts, req)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d, body: %q", http.StatusOK, rr.Code, rr.Body.String())
	}

	var userCount int
	if err := ts.db.QueryRow("SELECT COUNT(*) FROM admin_users").Scan(&userCount); err != nil {
		t.Fatalf("failed to query admin user count: %v", err)
	}
	if userCount != 1 {
		t.Fatalf("expected 1 admin user, got %d", userCount)
	}

	var username string
	if err := ts.db.QueryRow("SELECT username FROM admin_users LIMIT 1").Scan(&username); err != nil {
		t.Fatalf("failed to load admin username: %v", err)
	}
	if username != "adminuser" {
		t.Fatalf("expected lowercased username adminuser, got %q", username)
	}

	var siteTitle string
	if err := ts.db.QueryRow("SELECT value FROM settings WHERE key = 'site_title'").Scan(&siteTitle); err != nil {
		t.Fatalf("failed to load site_title: %v", err)
	}
	if siteTitle != "Onboarding Title" {
		t.Fatalf("expected site title to be saved, got %q", siteTitle)
	}

	// Setup endpoint should be hidden once an admin exists.
	setupReq := httptest.NewRequest(http.MethodGet, "/setup", nil)
	setupRes := httptest.NewRecorder()
	handler.ServeHTTP(setupRes, setupReq)
	if setupRes.Code != http.StatusNotFound {
		t.Fatalf("expected %d for /setup after onboarding, got %d", http.StatusNotFound, setupRes.Code)
	}

	// Index should no longer redirect to setup.
	indexReq := httptest.NewRequest(http.MethodGet, "/", nil)
	indexRes := httptest.NewRecorder()
	handler.ServeHTTP(indexRes, indexReq)
	if indexRes.Code != http.StatusOK {
		t.Fatalf("expected %d for / after onboarding, got %d", http.StatusOK, indexRes.Code)
	}
	if strings.Contains(indexRes.Body.String(), "Infoscope Setup") {
		t.Fatalf("unexpected setup UI in index response body")
	}
}
