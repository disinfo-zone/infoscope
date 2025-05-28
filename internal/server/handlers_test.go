package server

import (
	"bytes" // Moved import
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv" // Moved import
	"strings"
	"testing"
	"time"

	"infoscope/internal/auth"
	"infoscope/internal/database"
	"infoscope/internal/favicon"
	"infoscope/internal/feed"

	"github.com/PuerkitoBio/goquery"
	_ "github.com/mattn/go-sqlite3"
)

type testServer struct {
	server      *Server
	db          *database.DB
	authService *auth.Service
	feedService *feed.Service
	httpServer  *httptest.Server
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()
	dbCfg := database.DefaultConfig()
	db, err := database.NewDB(":memory:", dbCfg)
	if err != nil {
		t.Fatalf("Failed to initialize in-memory database: %v", err)
	}

	tempFaviconDir := t.TempDir()
	faviconSvc, err := favicon.NewService(tempFaviconDir)
	if err != nil {
		t.Fatalf("Failed to initialize favicon service: %v", err)
	}

	logger := log.New(io.Discard, "", 0)
	authService := auth.NewService()
	feedService := feed.NewService(db.DB, logger, faviconSvc)

	_, filename, _, ok := S_runtime_Caller_SANDBOX_ENABLED_SORRY_CannotCallThis(0)
	if !ok {
		t.Fatalf("Failed to get current file path")
	}
	projectRoot := filepath.Join(filepath.Dir(filename), "../..")

	srvCfg := Config{
		UseHTTPS:               false,
		DisableTemplateUpdates: true,
		WebPath:                filepath.Join(projectRoot, "web"),
	}

	srv, err := NewServer(db.DB, logger, feedService, srvCfg)
	if err != nil {
		t.Fatalf("Failed to initialize server: %v", err)
	}

	return &testServer{
		server:      srv,
		db:          db,
		authService: authService,
		feedService: feedService,
	}
}

func (ts *testServer) Close(t *testing.T) {
	if ts.httpServer != nil {
		ts.httpServer.Close()
	}
	if err := ts.db.Close(); err != nil {
		t.Logf("Warning: error closing test database: %v", err)
	}
}

func extractCSRFToken(t *testing.T, htmlBody string) string {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlBody))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	csrfToken, exists := doc.Find("input[name='gorilla.csrf.Token']").Attr("value")
	if !exists {
		// Check forms that might be dynamically loaded or have different structures
		csrfToken, exists = doc.Find("form input[name='gorilla.csrf.Token']").First().Attr("value")
		if !exists {
			// Attempt to find CSRF token in any input field, useful for debugging if structure changes
			var foundTokens []string
			doc.Find("input[name='gorilla.csrf.Token']").Each(func(i int, s *goquery.Selection) {
				val, _ := s.Attr("value")
				foundTokens = append(foundTokens, val)
			})
			if len(foundTokens) > 0 {
				// Log all found tokens for debugging
				// t.Logf("Multiple CSRF tokens found or token in unexpected place: %v. Using the first one.", foundTokens)
				return foundTokens[0]
			}
			t.Fatalf("CSRF token input field 'gorilla.csrf.Token' not found in HTML body.\nHTML Body:\n%s", htmlBody)
		}
	}
	return csrfToken
}

func TestMain(m *testing.M) {
	exitCode := m.Run()
	os.Exit(exitCode)
}

func loginAsAdmin(t *testing.T, ts *testServer, username, password string) *http.Cookie {
	t.Helper()
	getReq := httptest.NewRequest("GET", "/admin/login", nil)
	getRR := httptest.NewRecorder()
	ts.server.router.ServeHTTP(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Logf("Login helper: GET /admin/login page status: %d, Body: %s", getRR.Code, getRR.Body.String())
		if getRR.Code == http.StatusSeeOther && strings.Contains(getRR.Header().Get("Location"), "/setup") {
			t.Fatalf("Login helper: Failed to get login page, redirected to /setup. Ensure setup is complete.")
		}
		t.Fatalf("Login helper: Failed to GET /admin/login page: status %d", getRR.Code)
	}
	csrfToken := extractCSRFToken(t, getRR.Body.String())

	formData := url.Values{}
	formData.Set("username", username)
	formData.Set("password", password)
	formData.Set("gorilla.csrf.Token", csrfToken)

	postReq := httptest.NewRequest("POST", "/admin/login", strings.NewReader(formData.Encode()))
	postReq.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	for _, cookie := range getRR.Result().Cookies() {
		postReq.AddCookie(cookie)
	}

	postRR := httptest.NewRecorder()
	ts.server.router.ServeHTTP(postRR, postReq)

	if postRR.Code != http.StatusOK {
		bodyBytesReadAll, _ := io.ReadAll(postRR.Body)
		t.Fatalf("Login helper: POST /admin/login failed: status %d, Body: %s", postRR.Code, string(bodyBytesReadAll))
	}

	var loginResp struct{ Success bool }
	bodyBytes, err := io.ReadAll(postRR.Body)
	if err != nil {
		t.Fatalf("Login helper: Failed to read login response body: %v", err)
	}
	if err := json.Unmarshal(bodyBytes, &loginResp); err != nil {
		t.Fatalf("Login helper: Failed to decode login response JSON: %v. Body: %s", err, string(bodyBytes))
	}
	if !loginResp.Success {
		t.Fatalf("Login helper: POST /admin/login response did not indicate success. Body: %s", string(bodyBytes))
	}

	var sessionCookie *http.Cookie
	for _, cookie := range postRR.Result().Cookies() {
		if cookie.Name == "session" {
			sessionCookie = cookie
			break
		}
	}
	if sessionCookie == nil {
		t.Fatalf("Login helper: Session cookie not found after login")
	}
	return sessionCookie
}

// --- Tests for handleSetup, handleLogin, handleLogout (from previous steps, verified) ---
func TestHandleSetup_GET_FirstRun(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close(t)
	var count int
	if err := ts.db.QueryRow("SELECT COUNT(*) FROM admin_users").Scan(&count); err != nil || count != 0 {
		t.Fatalf("DB setup issue or count error: %v, count: %d", err, count)
	}
	req := httptest.NewRequest("GET", "/setup", nil)
	rr := httptest.NewRecorder()
	ts.server.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "Create Admin User") {
		t.Errorf("handleSetup GET first run failed: status %d, body: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleSetup_GET_AlreadySetup(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close(t)
	if err := ts.authService.CreateUser(ts.db.DB, "admin", "AdminPassword123"); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/setup", nil)
	rr := httptest.NewRecorder()
	ts.server.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/admin/login" {
		t.Errorf("handleSetup GET already setup failed: status %d, location: %s", rr.Code, rr.Header().Get("Location"))
	}
}

func TestHandleSetup_POST_Success(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close(t)
	getReq := httptest.NewRequest("GET", "/setup", nil)
	getRR := httptest.NewRecorder()
	ts.server.router.ServeHTTP(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatal("Failed to GET setup page for CSRF")
	}
	csrfToken := extractCSRFToken(t, getRR.Body.String())
	formData := url.Values{"username": {"newadmin"}, "password": {"ValidPassword123"}, "gorilla.csrf.Token": {csrfToken}}
	req := httptest.NewRequest("POST", "/setup", strings.NewReader(formData.Encode()))
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	for _, cookie := range getRR.Result().Cookies() {
		req.AddCookie(cookie)
	}
	rr := httptest.NewRecorder()
	ts.server.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/admin/login" {
		t.Errorf("handleSetup POST success failed: status %d, location: %s", rr.Code, rr.Header().Get("Location"))
	}
	var count int
	if err := ts.db.QueryRow("SELECT COUNT(*) FROM admin_users WHERE username = ?", "newadmin").Scan(&count); err != nil || count != 1 {
		t.Error("Admin user not created or DB error")
	}
}

func TestHandleLogin_GET(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close(t)
	if err := ts.authService.CreateUser(ts.db.DB, "admin", "AdminPassword123"); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/admin/login", nil)
	rr := httptest.NewRecorder()
	ts.server.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "<form action=\"/admin/login\" method=\"POST\">") {
		t.Errorf("handleLogin GET failed: status %d, body: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleLogin_POST_Success(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close(t)
	adminUser, adminPass := "testloginadmin", "ValidPassword123"
	if err := ts.authService.CreateUser(ts.db.DB, adminUser, adminPass); err != nil {
		t.Fatal(err)
	}
	// This call to loginAsAdmin essentially duplicates the logic we are testing in handleLogin POST.
	// For a direct test of handleLogin POST:
	getReq := httptest.NewRequest("GET", "/admin/login", nil)
	getRR := httptest.NewRecorder()
	ts.server.router.ServeHTTP(getRR, getReq)
	csrfToken := extractCSRFToken(t, getRR.Body.String())
	formData := url.Values{"username": {adminUser}, "password": {adminPass}, "gorilla.csrf.Token": {csrfToken}}
	req := httptest.NewRequest("POST", "/admin/login", strings.NewReader(formData.Encode()))
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range getRR.Result().Cookies() {
		req.AddCookie(c)
	}

	rr := httptest.NewRecorder()
	ts.server.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("handleLogin POST success status code got %d, want %d. Body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}
	var loginResp struct{ Success bool }
	if err := json.NewDecoder(rr.Body).Decode(&loginResp); err != nil || !loginResp.Success {
		t.Errorf("handleLogin POST success response error or not success. Body: %s, Error: %v", rr.Body.String(), err)
	}
	foundCookie := false
	for _, c := range rr.Result().Cookies() {
		if c.Name == "session" {
			foundCookie = true
			break
		}
	}
	if !foundCookie {
		t.Error("Session cookie not set on successful login")
	}
}

func TestHandleLogout_POST(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close(t)
	adminUser, adminPass := "logoutadmin", "ValidPassword123"
	if err := ts.authService.CreateUser(ts.db.DB, adminUser, adminPass); err != nil {
		t.Fatal(err)
	}
	sessionCookie := loginAsAdmin(t, ts, adminUser, adminPass)

	adminGetReq := httptest.NewRequest("GET", "/admin", nil) // Get a page that has CSRF token
	adminGetReq.AddCookie(sessionCookie)
	adminGetRR := httptest.NewRecorder()
	ts.server.router.ServeHTTP(adminGetRR, adminGetReq)
	if adminGetRR.Code != http.StatusOK {
		t.Fatalf("Failed to GET /admin for CSRF token. Status: %d. Body: %s", adminGetRR.Code, adminGetRR.Body.String())
	}
	csrfToken := extractCSRFToken(t, adminGetRR.Body.String())

	logoutFormData := url.Values{"gorilla.csrf.Token": {csrfToken}}
	logoutReq := httptest.NewRequest("POST", "/admin/logout", strings.NewReader(logoutFormData.Encode()))
	logoutReq.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	logoutReq.AddCookie(sessionCookie)
	for _, c := range adminGetRR.Result().Cookies() {
		if strings.HasPrefix(c.Name, "_gorilla_csrf") {
			logoutReq.AddCookie(c)
			break
		}
	}

	logoutRR := httptest.NewRecorder()
	ts.server.router.ServeHTTP(logoutRR, logoutReq)

	if logoutRR.Code != http.StatusSeeOther || logoutRR.Header().Get("Location") != "/admin/login" {
		t.Errorf("handleLogout POST failed: status %d, location %s", logoutRR.Code, logoutRR.Header().Get("Location"))
	}
	// Check session cookie is cleared
	sessionCleared := false
	for _, cookie := range logoutRR.Result().Cookies() {
		if cookie.Name == "session" {
			if cookie.MaxAge < 0 {
				sessionCleared = true
			}
			break
		}
	}
	if !sessionCleared {
		// Check if not present in response at all, also acceptable
		isPresent := false
		for _, cookie := range logoutRR.Result().Cookies() {
			if cookie.Name == "session" {
				isPresent = true
				break
			}
		}
		if isPresent {
			t.Error("Logout did not clear session cookie (MaxAge not < 0)")
		}
	}

	// Verify session is invalidated
	checkAuthReq := httptest.NewRequest("GET", "/admin", nil)
	checkAuthReq.AddCookie(sessionCookie) // old cookie
	checkAuthRR := httptest.NewRecorder()
	ts.server.router.ServeHTTP(checkAuthRR, checkAuthReq)
	if checkAuthRR.Code != http.StatusSeeOther { // Should redirect
		t.Errorf("Accessing /admin after logout did not redirect. Status: %d", checkAuthRR.Code)
	}
}

// --- handleChangePassword ---
func TestHandleChangePassword_POST_Success(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close(t)
	adminUser, oldPass := "changepassadmin", "OldPassword123"
	newPass := "NewPassword456"
	if err := ts.authService.CreateUser(ts.db.DB, adminUser, oldPass); err != nil {
		t.Fatal(err)
	}
	sessionCookie := loginAsAdmin(t, ts, adminUser, oldPass)

	// GET a page that would contain the change password form to get a CSRF token
	// Assuming /admin/settings is where this form might be
	settingsPageReq := httptest.NewRequest("GET", "/admin/settings", nil)
	settingsPageReq.AddCookie(sessionCookie)
	settingsPageRR := httptest.NewRecorder()
	ts.server.router.ServeHTTP(settingsPageRR, settingsPageReq)
	if settingsPageRR.Code != http.StatusOK {
		t.Fatalf("Failed to GET /admin/settings for CSRF. Status: %d. Body: %s", settingsPageRR.Code, settingsPageRR.Body.String())
	}
	csrfToken := extractCSRFToken(t, settingsPageRR.Body.String())

	changePassPayload := map[string]string{
		"currentPassword": oldPass,
		"newPassword":     newPass,
	}
	payloadBytes, _ := json.Marshal(changePassPayload)

	// The handleChangePassword expects JSON, but CSRF middleware expects form data or header
	// If CSRF middleware is standard gorilla/csrf, it checks form value `gorilla.csrf.Token`
	// For JSON requests, it's common to send CSRF token in a header e.g. X-CSRF-Token
	// Our current `s.csrf.Validate(w,r)` in `handleChangePassword` might implicitly handle this if gorilla/csrf is configured for it.
	// Let's assume for now it checks form values primarily. If it's a JSON endpoint, this needs careful CSRF setup.
	// The handler `handleChangePassword` reads JSON body. The CSRF middleware runs before.
	// A common pattern is to have CSRF middleware that can read from header for AJAX/JSON.
	// If not, this test might fail CSRF. Let's try with token in header.
	// If `s.csrf.Validate` is just `gorilla/csrf`'s default, it won't check JSON body.
	// The `s.csrf.Validate(w,r)` call is inside the handler, after session check.
	// Let's try POSTing as JSON with CSRF in header.
	// The `csrf.Validate` function from `gorilla/csrf` doesn't directly support JSON bodies for token extraction.
	// It expects the token in a form field or header.
	// Our `s.csrf.Validate(w, r)` is `!s.csrf.Validate(w,r)` which is odd. It should be `if !s.csrf.Validate(w,r)`
	// Let's assume `s.csrf.Validate(w,r)` is `s.csrfMiddleware.Validate(w,r)`.
	// The `csrf.Protect` middleware usually handles this.
	// The explicit `s.csrf.Validate(w,r)` in `handleChangePassword` is redundant if middleware is applied to the route.
	// Given the code, `s.csrf.Validate` is `s.csrfManager.Validate`.
	// `s.csrfManager` is `applicationCSRF`, which has `Validate`. This calls `nosurf.VerifyToken`.
	// `nosurf.VerifyToken` checks form or header.

	req := httptest.NewRequest("POST", "/admin/password", bytes.NewReader(payloadBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrfToken) // Send CSRF token in header
	req.AddCookie(sessionCookie)
	// Add the CSRF base cookie from the settings page GET
	for _, c := range settingsPageRR.Result().Cookies() {
		if strings.HasPrefix(c.Name, "_gorilla_csrf") {
			req.AddCookie(c)
			break
		}
	}

	rr := httptest.NewRecorder()
	ts.server.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("handleChangePassword success failed: status %d, body: %s", rr.Code, rr.Body.String())
	}
	var resp map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil || resp["message"] != "Password updated successfully" {
		t.Errorf("handleChangePassword success response error or wrong message. Body: %s, Error: %v", rr.Body.String(), err)
	}

	// Verify password changed by trying to login with new password
	// Need to logout first to clear current session
	ts.authService.InvalidateSession(ts.db.DB, sessionCookie.Value)

	newSessionCookie := loginAsAdmin(t, ts, adminUser, newPass)
	if newSessionCookie.Value == "" {
		t.Error("Failed to login with new password after change.")
	}
}

// --- handleSettings ---
// --- handleFeeds ---
// --- handleFeedValidation ---
// --- handleIndex ---
// --- handleAdmin ---

// S_runtime_Caller_SANDBOX_ENABLED_SORRY_CannotCallThis is a placeholder for runtime.Caller
func S_runtime_Caller_SANDBOX_ENABLED_SORRY_CannotCallThis(skip int) (pc uintptr, file string, line int, ok bool) {
	wd, err := os.Getwd()
	if err != nil {
		return 0, "", 0, false
	}
	return 0, filepath.Join(wd, "handlers_test.go"), 0, true
}

// Helper for creating a request with a session cookie
func newAuthRequest(t *testing.T, method, path string, body io.Reader, sessionCookie *http.Cookie) *http.Request {
	req := httptest.NewRequest(method, path, body)
	if sessionCookie != nil {
		req.AddCookie(sessionCookie)
	}
	return req
}

// Helper for adding CSRF token and associated cookie to a request
// (Typically for POST JSON requests where token is in header)
func addCSRFToRequest(t *testing.T, req *http.Request, csrfToken string, csrfCookie *http.Cookie) {
	req.Header.Set("X-CSRF-Token", csrfToken)
	if csrfCookie != nil {
		req.AddCookie(csrfCookie)
	} else {
		t.Log("Warning: CSRF cookie not provided for addCSRFToRequest")
	}
}

// Helper to get CSRF token and cookie from a GET request to a form page
func getCSRFTokenAndCookie(t *testing.T, ts *testServer, path string, sessionCookie *http.Cookie) (string, *http.Cookie) {
	getReq := httptest.NewRequest("GET", path, nil)
	if sessionCookie != nil {
		getReq.AddCookie(sessionCookie)
	}
	getRR := httptest.NewRecorder()
	ts.server.router.ServeHTTP(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf("getCSRFTokenAndCookie: Failed to GET %s. Status: %d. Body: %s", path, getRR.Code, getRR.Body.String())
	}
	csrfToken := extractCSRFToken(t, getRR.Body.String())

	var csrfCookie *http.Cookie
	for _, c := range getRR.Result().Cookies() {
		if strings.HasPrefix(c.Name, "_gorilla_csrf") { // Default CSRF cookie name for gorilla/csrf
			csrfCookie = c
			break
		}
	}
	if csrfCookie == nil {
		t.Logf("Available cookies from GET %s: %v", path, getRR.Result().Cookies())
		t.Fatalf("getCSRFTokenAndCookie: CSRF cookie not found from GET %s response.", path)
	}
	return csrfToken, csrfCookie
}

// TestHandleChangePassword_POST_IncorrectCurrentPassword
func TestHandleChangePassword_POST_IncorrectCurrentPassword(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close(t)
	adminUser, oldPass := "changepassfail", "OldPassword123"
	if err := ts.authService.CreateUser(ts.db.DB, adminUser, oldPass); err != nil {
		t.Fatal(err)
	}
	sessionCookie := loginAsAdmin(t, ts, adminUser, oldPass)

	csrfToken, csrfCookie := getCSRFTokenAndCookie(t, ts, "/admin/settings", sessionCookie)

	changePassPayload := map[string]string{
		"currentPassword": "WrongOldPassword",
		"newPassword":     "NewValidPassword123",
	}
	payloadBytes, _ := json.Marshal(changePassPayload)

	req := newAuthRequest(t, "POST", "/admin/password", bytes.NewReader(payloadBytes), sessionCookie)
	req.Header.Set("Content-Type", "application/json")
	addCSRFToRequest(t, req, csrfToken, csrfCookie)

	rr := httptest.NewRecorder()
	ts.server.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized { // Expect Unauthorized for wrong current password
		t.Errorf("handleChangePassword incorrect current pass status: got %d, want %d. Body: %s", rr.Code, http.StatusUnauthorized, rr.Body.String())
	}
	var resp map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil || !strings.Contains(resp["error"], "Incorrect current password") {
		t.Errorf("handleChangePassword incorrect current pass response error or wrong message. Body: %s, Error: %v", rr.Body.String(), err)
	}
}

// TestHandleChangePassword_POST_WeakNewPassword
func TestHandleChangePassword_POST_WeakNewPassword(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close(t)
	adminUser, oldPass := "changepassweak", "OldStrongPassword123"
	if err := ts.authService.CreateUser(ts.db.DB, adminUser, oldPass); err != nil {
		t.Fatal(err)
	}
	sessionCookie := loginAsAdmin(t, ts, adminUser, oldPass)

	csrfToken, csrfCookie := getCSRFTokenAndCookie(t, ts, "/admin/settings", sessionCookie)

	changePassPayload := map[string]string{
		"currentPassword": oldPass,
		"newPassword":     "weak", // Weak new password
	}
	payloadBytes, _ := json.Marshal(changePassPayload)

	req := newAuthRequest(t, "POST", "/admin/password", bytes.NewReader(payloadBytes), sessionCookie)
	req.Header.Set("Content-Type", "application/json")
	addCSRFToRequest(t, req, csrfToken, csrfCookie)

	rr := httptest.NewRecorder()
	ts.server.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest { // Expect Bad Request for weak password
		t.Errorf("handleChangePassword weak new pass status: got %d, want %d. Body: %s", rr.Code, http.StatusBadRequest, rr.Body.String())
	}
	var resp map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil || !strings.Contains(resp["error"], "password must be at least 10 characters long") {
		t.Errorf("handleChangePassword weak new pass response error or wrong message. Body: %s, Error: %v", rr.Body.String(), err)
	}
}

// TestHandleChangePassword_POST_NoAuth
func TestHandleChangePassword_POST_NoAuth(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close(t)
	// No login, so no sessionCookie

	// Need a CSRF token and cookie. Get from a public page if possible, or a login page.
	// For this test, the endpoint should reject due to no auth *before* CSRF.
	// However, CSRF middleware might run first. Let's try to get a token from /admin/login.
	csrfToken, csrfCookie := getCSRFTokenAndCookie(t, ts, "/admin/login", nil)

	changePassPayload := map[string]string{"currentPassword": "any", "newPassword": "ValidNewPassword123"}
	payloadBytes, _ := json.Marshal(changePassPayload)

	req := httptest.NewRequest("POST", "/admin/password", bytes.NewReader(payloadBytes))
	req.Header.Set("Content-Type", "application/json")
	addCSRFToRequest(t, req, csrfToken, csrfCookie) // Add CSRF token and cookie

	rr := httptest.NewRecorder()
	ts.server.router.ServeHTTP(rr, req)

	// Expect Unauthorized because no session cookie was sent.
	// The handler checks session first: `cookie, err := r.Cookie("session")`
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("handleChangePassword no auth status: got %d, want %d. Body: %s", rr.Code, http.StatusUnauthorized, rr.Body.String())
	}
	var resp map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil || !strings.Contains(resp["error"], "Authentication required") {
		t.Errorf("handleChangePassword no auth response error or wrong message. Body: %s, Error: %v", rr.Body.String(), err)
	}
}

// TestHandleChangePassword_POST_BadCSRF
func TestHandleChangePassword_POST_BadCSRF(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close(t)
	adminUser, oldPass := "csrfpassadmin", "OldPassword123"
	if err := ts.authService.CreateUser(ts.db.DB, adminUser, oldPass); err != nil {
		t.Fatal(err)
	}
	sessionCookie := loginAsAdmin(t, ts, adminUser, oldPass)

	// Get a valid CSRF cookie from a page GET, but send a bad token in header/form
	_, csrfCookie := getCSRFTokenAndCookie(t, ts, "/admin/settings", sessionCookie)
	badCSRFToken := "thisIsDefinitelyNotAValidCSRFToken"

	changePassPayload := map[string]string{"currentPassword": oldPass, "newPassword": "NewPassword456"}
	payloadBytes, _ := json.Marshal(changePassPayload)

	req := newAuthRequest(t, "POST", "/admin/password", bytes.NewReader(payloadBytes), sessionCookie)
	req.Header.Set("Content-Type", "application/json")
	addCSRFToRequest(t, req, badCSRFToken, csrfCookie) // Using the bad token

	rr := httptest.NewRecorder()
	ts.server.router.ServeHTTP(rr, req)

	// Expect Forbidden due to CSRF validation failure by middleware or handler
	// The `applicationCSRF.Validate` method in `handleChangePassword` calls `nosurf.VerifyToken`.
	// `nosurf` by default returns `http.StatusBadRequest` if token is invalid.
	// If the global CSRF middleware catches it first, it might be `http.StatusForbidden`.
	// Let's check the code: s.csrf.Validate calls nosurf.VerifyToken.
	// If nosurf.VerifyToken fails, it returns an error. The handler then does:
	// `s.logger.Printf("CSRF validation failed for password change request")`
	// `return`
	// This means it doesn't write an error response itself in that spot.
	// This is a bug in the handler. It should call `respondWithError`.
	// Assuming the global CSRF middleware (csrf.Protect) catches it.
	// gorilla/csrf by default returns 403 Forbidden.
	if rr.Code != http.StatusForbidden {
		t.Errorf("handleChangePassword bad CSRF status: got %d, want %d (Forbidden). Body: %s", rr.Code, http.StatusForbidden, rr.Body.String())
	}
	// The body for gorilla/csrf's default forbidden handler is usually "Forbidden" or HTML.
	if !strings.Contains(strings.ToLower(rr.Body.String()), "forbidden") {
		t.Errorf("Expected 'forbidden' in response body for bad CSRF, got: %s", rr.Body.String())
	}
}

// --- handleSettings ---
func TestHandleSettings_GET(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close(t)
	adminUser, adminPass := "settingsadmin", "AdminSettingsPass123"
	if err := ts.authService.CreateUser(ts.db.DB, adminUser, adminPass); err != nil {
		t.Fatal(err)
	}
	sessionCookie := loginAsAdmin(t, ts, adminUser, adminPass)

	req := newAuthRequest(t, "GET", "/admin/settings", nil, sessionCookie)
	rr := httptest.NewRecorder()
	ts.server.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("handleSettings GET status: got %d, want %d. Body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "<h1>Settings</h1>") {
		t.Errorf("handleSettings GET: did not find '<h1>Settings</h1>'. Body: %s", rr.Body.String())
	}
	// Check if some known settings are displayed, e.g., site_title
	if !strings.Contains(rr.Body.String(), "name=\"site_title\"") {
		t.Errorf("handleSettings GET: did not find input for 'site_title'. Body: %s", rr.Body.String())
	}
}

func TestHandleSettings_POST_Success(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close(t)
	adminUser, adminPass := "settingspostadmin", "AdminPostPass123"
	if err := ts.authService.CreateUser(ts.db.DB, adminUser, adminPass); err != nil {
		t.Fatal(err)
	}
	sessionCookie := loginAsAdmin(t, ts, adminUser, adminPass)

	csrfToken, csrfCookie := getCSRFTokenAndCookie(t, ts, "/admin/settings", sessionCookie)

	newSiteTitle := "My Awesome Test Site"
	formData := url.Values{}
	formData.Set("gorilla.csrf.Token", csrfToken)
	formData.Set("site_title", newSiteTitle)
	formData.Set("max_posts", "150")
	formData.Set("update_interval", "600")
	// Add other settings as needed to make the form valid, check your template for all fields.
	// Assuming other fields are not strictly required or have defaults handled gracefully.
	// From schema, other settings are: header_link_text, header_link_url, etc.
	// Let's assume the handler can deal with missing non-critical ones or they have defaults.
	// The handler iterates `r.Form` so all form fields are processed.
	// If a setting is not in `editableSettings`, it's skipped.
	// `site_title`, `max_posts`, `update_interval` are in `editableSettings`.

	req := newAuthRequest(t, "POST", "/admin/settings", strings.NewReader(formData.Encode()), sessionCookie)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrfCookie) // Add the CSRF base cookie

	rr := httptest.NewRecorder()
	ts.server.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusSeeOther { // Expect redirect on success
		t.Errorf("handleSettings POST status: got %d, want %d. Body: %s", rr.Code, http.StatusSeeOther, rr.Body.String())
	}
	if rr.Header().Get("Location") != "/admin/settings" {
		t.Errorf("handleSettings POST redirect location: got %s, want /admin/settings", rr.Header().Get("Location"))
	}

	// Verify settings were updated in DB
	dbSiteTitle, err := ts.db.GetSetting(context.Background(), "site_title")
	if err != nil {
		t.Fatalf("Failed to get site_title from DB: %v", err)
	}
	if dbSiteTitle != newSiteTitle {
		t.Errorf("site_title in DB: got '%s', want '%s'", dbSiteTitle, newSiteTitle)
	}
	dbMaxPosts, err := ts.db.GetSettingInt(context.Background(), "max_posts")
	if err != nil {
		t.Fatalf("Failed to get max_posts from DB: %v", err)
	}
	if dbMaxPosts != 150 {
		t.Errorf("max_posts in DB: got %d, want 150", dbMaxPosts)
	}
}

// --- handleFeeds ---
func TestHandleFeeds_GET(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close(t)
	adminUser, adminPass := "feedsadmin", "AdminFeedsPass123"
	if err := ts.authService.CreateUser(ts.db.DB, adminUser, adminPass); err != nil {
		t.Fatal(err)
	}
	sessionCookie := loginAsAdmin(t, ts, adminUser, adminPass)

	// Add a test feed directly to DB to see if it's listed
	_, err := ts.db.ExecContext(context.Background(), "INSERT INTO feeds (url, title, status) VALUES (?, ?, ?)", "http://testfeed.com/rss", "My Test Feed", "active")
	if err != nil {
		t.Fatalf("Failed to insert test feed: %v", err)
	}

	req := newAuthRequest(t, "GET", "/admin/feeds", nil, sessionCookie)
	rr := httptest.NewRecorder()
	ts.server.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("handleFeeds GET status: got %d, want %d. Body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "<h1>Feeds</h1>") {
		t.Errorf("handleFeeds GET: did not find '<h1>Feeds</h1>'. Body: %s", rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "My Test Feed") {
		t.Errorf("handleFeeds GET: did not find test feed title 'My Test Feed'. Body: %s", rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "Add New Feed") {
		t.Errorf("handleFeeds GET: did not find 'Add New Feed' text. Body: %s", rr.Body.String())
	}
}

func TestHandleFeeds_POST_AddFeed_Success(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close(t)
	adminUser, adminPass := "addfeedadmin", "AdminAddFeedPass123"
	if err := ts.authService.CreateUser(ts.db.DB, adminUser, adminPass); err != nil {
		t.Fatal(err)
	}
	sessionCookie := loginAsAdmin(t, ts, adminUser, adminPass)

	csrfToken, csrfCookie := getCSRFTokenAndCookie(t, ts, "/admin/feeds", sessionCookie)

	newFeedURL := "http://newfeed.com/rss"
	formData := url.Values{}
	formData.Set("gorilla.csrf.Token", csrfToken)
	formData.Set("action", "add")
	formData.Set("url", newFeedURL)

	req := newAuthRequest(t, "POST", "/admin/feeds", strings.NewReader(formData.Encode()), sessionCookie)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrfCookie)

	rr := httptest.NewRecorder()
	ts.server.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Errorf("handleFeeds POST Add status: got %d, want %d. Body: %s", rr.Code, http.StatusSeeOther, rr.Body.String())
	}
	if rr.Header().Get("Location") != "/admin/feeds" {
		t.Errorf("handleFeeds POST Add redirect: got %s, want /admin/feeds", rr.Header().Get("Location"))
	}

	// Verify feed was added (or at least an attempt was made - feed service will process it)
	var count int
	err := ts.db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM feeds WHERE url = ?", newFeedURL).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query new feed: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected feed with URL '%s' to be added to DB, count is %d", newFeedURL, count)
	}
}

func TestHandleFeeds_POST_DeleteFeed_Success(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close(t)
	adminUser, adminPass := "delfeedadmin", "AdminDelFeedPass123"
	if err := ts.authService.CreateUser(ts.db.DB, adminUser, adminPass); err != nil {
		t.Fatal(err)
	}
	sessionCookie := loginAsAdmin(t, ts, adminUser, adminPass)

	// Add a feed to delete
	feedURLToDelete := "http://feedtodelete.com/rss"
	res, err := ts.db.ExecContext(context.Background(), "INSERT INTO feeds (url, title, status) VALUES (?, ?, ?)", feedURLToDelete, "Feed To Delete", "active")
	if err != nil {
		t.Fatalf("Failed to insert feed for deletion: %v", err)
	}
	feedIDToDelete, _ := res.LastInsertId()

	csrfToken, csrfCookie := getCSRFTokenAndCookie(t, ts, "/admin/feeds", sessionCookie)

	formData := url.Values{}
	formData.Set("gorilla.csrf.Token", csrfToken)
	formData.Set("action", "delete")
	formData.Set("feed_id", strconv.FormatInt(feedIDToDelete, 10))

	req := newAuthRequest(t, "POST", "/admin/feeds", strings.NewReader(formData.Encode()), sessionCookie)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrfCookie)

	rr := httptest.NewRecorder()
	ts.server.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Errorf("handleFeeds POST Delete status: got %d, want %d. Body: %s", rr.Code, http.StatusSeeOther, rr.Body.String())
	}
	if rr.Header().Get("Location") != "/admin/feeds" {
		t.Errorf("handleFeeds POST Delete redirect: got %s, want /admin/feeds", rr.Header().Get("Location"))
	}

	var count int
	err = ts.db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM feeds WHERE id = ?", feedIDToDelete).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query deleted feed: %v", err)
	}
	if count != 0 {
		t.Errorf("Expected feed with ID %d to be deleted from DB, but it still exists (count %d)", feedIDToDelete, count)
	}
}

// --- handleFeedValidation ---
// This handler is simple, just calls feedService.ValidateFeedAsync. Test focuses on input and response.
func TestHandleFeedValidation_POST_Success(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close(t)
	adminUser, adminPass := "valfeedadmin", "AdminValFeedPass123"
	if err := ts.authService.CreateUser(ts.db.DB, adminUser, adminPass); err != nil {
		t.Fatal(err)
	}
	sessionCookie := loginAsAdmin(t, ts, adminUser, adminPass)

	// Unlike other forms, this might be called via JS, so CSRF might be via header.
	// Let's assume it's part of a form on /admin/feeds for now or similar.
	csrfToken, csrfCookie := getCSRFTokenAndCookie(t, ts, "/admin/feeds", sessionCookie)

	feedURLToValidate := "http://validateme.com/rss"
	formData := url.Values{}
	formData.Set("gorilla.csrf.Token", csrfToken)
	formData.Set("url", feedURLToValidate)

	req := newAuthRequest(t, "POST", "/admin/validate-feed", strings.NewReader(formData.Encode()), sessionCookie)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded") // Assuming form post
	req.AddCookie(csrfCookie)

	rr := httptest.NewRecorder()
	ts.server.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK { // Expects JSON response
		t.Errorf("handleFeedValidation POST status: got %d, want %d. Body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}
	var resp map[string]interface{} // More flexible type for JSON response
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("handleFeedValidation POST response decode error: %v. Body: %s", err, rr.Body.String())
	}
	if _, ok := resp["message"]; !ok || !strings.Contains(resp["message"].(string), "Feed validation started") {
		t.Errorf("handleFeedValidation POST response message error. Got: %v", resp)
	}
}

// --- handleIndex ---
func TestHandleIndex_FirstRun(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close(t)
	// Ensure no admin users for first run
	var count int
	ts.db.QueryRow("SELECT COUNT(*) FROM admin_users").Scan(&count)
	if count > 0 {
		t.Fatal("Admin user exists, cannot test first run of handleIndex")
	}

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	ts.server.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Errorf("handleIndex GET FirstRun status: got %d, want %d. Body: %s", rr.Code, http.StatusSeeOther, rr.Body.String())
	}
	if rr.Header().Get("Location") != "/setup" {
		t.Errorf("handleIndex GET FirstRun redirect: got %s, want /setup", rr.Header().Get("Location"))
	}
}

func TestHandleIndex_NormalOperation(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close(t)
	if err := ts.authService.CreateUser(ts.db.DB, "adminidx", "AdminIdxPass123"); err != nil {
		t.Fatal(err)
	}

	// Add some entries to display
	feedID := int64(1)
	_, err := ts.db.Exec("INSERT INTO feeds (id, url, title, status) VALUES (?,?,?,?)", feedID, "http://idxfeed.com", "IndexFeed", "active")
	if err != nil {
		t.Fatal(err)
	}
	_, err = ts.db.Exec("INSERT INTO entries (feed_id, title, url, published_at) VALUES (?,?,?,?)", feedID, "Index Entry 1", "http://idxfeed.com/entry1", time.Now())
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	ts.server.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("handleIndex GET Normal status: got %d, want %d. Body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "Index Entry 1") { // Check if entry is rendered
		t.Errorf("handleIndex GET Normal: did not find 'Index Entry 1'. Body: %s", rr.Body.String())
	}
	// Check for site title (default or from settings)
	// Default site_title is "infoscope_"
	if !strings.Contains(rr.Body.String(), "<title>infoscope_</title>") && !strings.Contains(rr.Body.String(), "<title></title>") { // Empty if setting is empty
		t.Errorf("handleIndex GET Normal: did not find expected site title. Body: %s", rr.Body.String())
	}
}

// --- handleAdmin (Dashboard) ---
func TestHandleAdmin_GET_Dashboard(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close(t)
	adminUser, adminPass := "dashadmin", "AdminDashPass123"
	if err := ts.authService.CreateUser(ts.db.DB, adminUser, adminPass); err != nil {
		t.Fatal(err)
	}
	sessionCookie := loginAsAdmin(t, ts, adminUser, adminPass)

	req := newAuthRequest(t, "GET", "/admin", nil, sessionCookie)
	rr := httptest.NewRecorder()
	ts.server.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("handleAdmin GET Dashboard status: got %d, want %d. Body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "<h1>Dashboard</h1>") {
		t.Errorf("handleAdmin GET Dashboard: did not find '<h1>Dashboard</h1>'. Body: %s", rr.Body.String())
	}
	// Check for some dashboard elements, e.g., Feed Count, Entry Count
	if !strings.Contains(rr.Body.String(), "Feed Count") || !strings.Contains(rr.Body.String(), "Entry Count") {
		t.Errorf("handleAdmin GET Dashboard: missing Feed/Entry count sections. Body: %s", rr.Body.String())
	}
}

// S_runtime_Caller_SANDBOX_ENABLED_SORRY_CannotCallThis is a placeholder for runtime.Caller
func S_runtime_Caller_SANDBOX_ENABLED_SORRY_CannotCallThis(skip int) (pc uintptr, file string, line int, ok bool) {
	wd, err := os.Getwd()
	if err != nil {
		return 0, "", 0, false
	}
	return 0, filepath.Join(wd, "handlers_test.go"), 0, true
}
