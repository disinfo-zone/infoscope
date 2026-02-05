package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSetupTemplateRenders(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close(t)

	req := httptest.NewRequest(http.MethodGet, "/setup", nil)
	rr := httptest.NewRecorder()

	data := LoginPageRenderData{
		CSRFToken: "test-token",
		Settings:  map[string]string{},
		Error:     "",
	}

	if err := ts.server.renderTemplate(rr, req, "setup.html", data); err != nil {
		t.Fatalf("Failed to render setup template: %v", err)
	}

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", rr.Code)
	}
	if body := rr.Body.String(); body == "" {
		t.Fatal("Expected non-empty response body for setup template")
	}
}
