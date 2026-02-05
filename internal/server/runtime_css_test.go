package server

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSanitizeCSSSize(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"50px", "50px"},
		{"1.5rem", "1.5rem"},
		{"100%", "100%"},
		{"0", "0"},
		{"auto", "auto"},
		{"  2em ", "2em"},
		{"calc(100% - 1rem)", ""},
		{"expression(alert(1))", ""},
		{"10", ""},
		{"-10px", ""},
		{"", ""},
	}

	for _, tt := range tests {
		if got := sanitizeCSSSize(tt.input); got != tt.want {
			t.Errorf("sanitizeCSSSize(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestHandleRuntimeCSSUsesSetting(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close(t)

	_, err := ts.db.Exec(`INSERT OR REPLACE INTO settings (key, value, type) VALUES ('footer_image_height', '50px', 'string')`)
	if err != nil {
		t.Fatalf("failed to seed footer_image_height: %v", err)
	}

	req := httptest.NewRequest("GET", "/static/runtime.css", nil)
	rr := httptest.NewRecorder()
	ts.server.handleRuntimeCSS(rr, req)

	if rr.Code != 200 {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/css") {
		t.Fatalf("expected text/css content type, got %q", ct)
	}

	body := rr.Body.String()
	if !strings.Contains(body, "--footer-image-height:50px") {
		t.Fatalf("expected footer height in css, got %q", body)
	}
}

