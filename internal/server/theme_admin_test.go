package server

import "testing"

func TestGetAvailableAdminThemesIncludesAurora(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close(t)

	adminThemes := ts.server.getAvailableAdminThemes()
	if !ts.server.containsString(adminThemes, "aurora") {
		t.Fatalf("expected 'aurora' to be available in admin themes: %v", adminThemes)
	}
	if !ts.server.containsString(adminThemes, "terminal") {
		t.Fatalf("expected 'terminal' to be available in admin themes: %v", adminThemes)
	}
}

func TestThemeCSSAdminFallsBackForInvalidThemeSelection(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close(t)

	funcMap := ts.server.registerTemplateFuncs()
	themeCSSAdmin, ok := funcMap["themeCSSAdmin"].(func(map[string]string, string) string)
	if !ok {
		t.Fatal("themeCSSAdmin template function has unexpected type")
	}

	settings := map[string]string{"admin_theme": "aurora"}

	if got := themeCSSAdmin(settings, "variables.css"); got != "/static/css/themes/aurora/variables.css" {
		t.Fatalf("expected aurora variables.css path, got %q", got)
	}
	if got := themeCSSAdmin(settings, "admin.css"); got != "/static/css/themes/aurora/admin.css" {
		t.Fatalf("expected aurora admin.css path, got %q", got)
	}

	invalidSettings := map[string]string{"admin_theme": "missing_theme"}
	if got := themeCSSAdmin(invalidSettings, "variables.css"); got != "/static/css/themes/terminal/variables.css" {
		t.Fatalf("expected fallback terminal variables.css path for invalid theme, got %q", got)
	}
	if got := themeCSSAdmin(invalidSettings, "admin.css"); got != "/static/css/themes/terminal/admin.css" {
		t.Fatalf("expected fallback terminal admin.css path for invalid theme, got %q", got)
	}
}
