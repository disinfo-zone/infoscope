package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSettingsTrackingCodeSavedAndRendered(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close(t)

	session := createAdminSession(t, ts)
	handler := ts.server.Routes()

	payload := `{
		"siteTitle":"Tracking Test",
		"siteURL":"https://example.com",
		"maxPosts":10,
		"updateInterval":300,
		"headerLinkText":"",
		"headerLinkURL":"",
		"footerLinkText":"",
		"footerLinkURL":"",
		"footerImageURL":"",
		"footerImageHeight":"50px",
		"trackingCode":"<script async src=\"https://www.googletagmanager.com/gtag/js?id=G-TEST123\"></script><script>alert('xss')</script><img src=\"https://analytics.example.com/pixel.gif\" width=\"1\" height=\"1\" alt=\"\">",
		"faviconURL":"favicon.ico",
		"timezone":"UTC",
		"metaDescription":"Tracking test",
		"metaImageURL":"",
		"theme":"terminal",
		"publicTheme":"terminal",
		"adminTheme":"terminal",
		"backupEnabled":false,
		"backupIntervalHours":24,
		"backupRetentionDays":30,
		"showBlogName":false,
		"showBodyText":false,
		"bodyTextLength":200,
		"allowPublicThemeSelection":false,
		"publicAvailableThemes":"terminal"
	}`

	saveReq := httptest.NewRequest(http.MethodPost, "/admin/settings", strings.NewReader(payload))
	saveReq.Header.Set("Content-Type", "application/json")
	saveReq.AddCookie(&http.Cookie{Name: "session", Value: session.ID})
	addCSRFToken(t, ts, saveReq)
	saveRes := httptest.NewRecorder()
	handler.ServeHTTP(saveRes, saveReq)

	if saveRes.Code != http.StatusOK {
		t.Fatalf("expected settings save status %d, got %d (body: %s)", http.StatusOK, saveRes.Code, saveRes.Body.String())
	}

	var storedTracking string
	if err := ts.db.QueryRow("SELECT value FROM settings WHERE key = 'tracking_code'").Scan(&storedTracking); err != nil {
		t.Fatalf("failed to load tracking_code setting: %v", err)
	}
	if !strings.Contains(storedTracking, `src="https://www.googletagmanager.com/gtag/js?id=G-TEST123"`) {
		t.Fatalf("expected stored tracking code to preserve external script, got: %s", storedTracking)
	}
	if !strings.Contains(storedTracking, `src="https://analytics.example.com/pixel.gif"`) {
		t.Fatalf("expected stored tracking code to preserve tracking pixel, got: %s", storedTracking)
	}
	if strings.Contains(storedTracking, "alert('xss')") {
		t.Fatalf("expected stored tracking code to strip inline script payload, got: %s", storedTracking)
	}

	indexReq := httptest.NewRequest(http.MethodGet, "/", nil)
	indexRes := httptest.NewRecorder()
	handler.ServeHTTP(indexRes, indexReq)

	if indexRes.Code != http.StatusOK {
		t.Fatalf("expected index status %d, got %d", http.StatusOK, indexRes.Code)
	}

	body := indexRes.Body.String()
	if !strings.Contains(body, `https://www.googletagmanager.com/gtag/js?id=G-TEST123`) {
		t.Fatalf("expected index page to render stored analytics script, body: %s", body)
	}
	if !strings.Contains(body, `https://analytics.example.com/pixel.gif`) {
		t.Fatalf("expected index page to render stored analytics pixel, body: %s", body)
	}
	if strings.Contains(body, "alert('xss')") {
		t.Fatalf("unexpected inline tracking script payload rendered on index: %s", body)
	}
}
