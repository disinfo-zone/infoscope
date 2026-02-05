package server

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"infoscope/internal/auth"
	"infoscope/internal/database"
	"infoscope/internal/favicon"
	"infoscope/internal/feed"
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

	_, filename, _, ok := runtime.Caller(0)
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

func TestMain(m *testing.M) {
	exitCode := m.Run()
	os.Exit(exitCode)
}

// Basic test that verifies the test setup works
func TestServerCreation(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close(t)

	if ts.server == nil {
		t.Error("Failed to create server")
	}
	if ts.db == nil {
		t.Error("Failed to create database")
	}
	if ts.authService == nil {
		t.Error("Failed to create auth service")
	}
	if ts.feedService == nil {
		t.Error("Failed to create feed service")
	}
}

func TestIndexRedirectsToSetupOnFirstRun(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close(t)

	handler := ts.server.Routes()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected %d, got %d", http.StatusSeeOther, rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/setup" {
		t.Fatalf("expected redirect to /setup, got %q", loc)
	}
}

func TestClickEndpointDoesNotRequireCSRF(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close(t)

	res, err := ts.db.Exec("INSERT INTO feeds (url, title) VALUES (?, ?)", "http://example.com/feed", "Example Feed")
	if err != nil {
		t.Fatalf("failed to insert feed: %v", err)
	}
	feedID, _ := res.LastInsertId()

	entryRes, err := ts.db.Exec("INSERT INTO entries (feed_id, title, url, published_at) VALUES (?, ?, ?, ?)",
		feedID, "Entry", "http://example.com/entry", time.Now())
	if err != nil {
		t.Fatalf("failed to insert entry: %v", err)
	}
	entryID, _ := entryRes.LastInsertId()

	handler := ts.server.Routes()
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/click?id=%d", entryID), nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
	}

	var clicks int
	if err := ts.db.QueryRow("SELECT click_count FROM clicks WHERE entry_id = ?", entryID).Scan(&clicks); err != nil {
		t.Fatalf("failed to read click count: %v", err)
	}
	if clicks != 1 {
		t.Fatalf("expected click_count 1, got %d", clicks)
	}
}

func TestFeedAPIDeleteRequiresCSRF(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close(t)

	session := createAdminSession(t, ts)

	res, err := ts.db.Exec("INSERT INTO feeds (url, title) VALUES (?, ?)", "http://example.com/delete", "Delete Feed")
	if err != nil {
		t.Fatalf("failed to insert feed: %v", err)
	}
	feedID, _ := res.LastInsertId()

	handler := ts.server.Routes()

	req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/admin/api/feeds/%d", feedID), nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: session.ID})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected %d, got %d", http.StatusForbidden, rr.Code)
	}

	reqWithCSRF := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/admin/api/feeds/%d", feedID), nil)
	reqWithCSRF.AddCookie(&http.Cookie{Name: "session", Value: session.ID})
	addCSRFToken(t, ts, reqWithCSRF)
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, reqWithCSRF)

	if rr2.Code != http.StatusNoContent {
		t.Fatalf("expected %d, got %d", http.StatusNoContent, rr2.Code)
	}
}

func addCSRFToken(t *testing.T, ts *testServer, req *http.Request) {
	t.Helper()
	rr := httptest.NewRecorder()
	token := ts.server.csrf.Token(rr, req)
	resp := rr.Result()
	var csrfCookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == ts.server.csrf.config.Cookie {
			csrfCookie = c
			break
		}
	}
	if csrfCookie == nil {
		t.Fatalf("csrf cookie not set")
	}
	req.AddCookie(csrfCookie)
	req.Header.Set(ts.server.csrf.config.Header, token)
}

func createAdminSession(t *testing.T, ts *testServer) *auth.Session {
	t.Helper()
	const username = "admin"
	const password = "Str0ng!Passw0rd123"

	if err := ts.server.auth.CreateUser(ts.db.DB, username, password); err != nil {
		t.Fatalf("failed to create admin user: %v", err)
	}
	session, err := ts.server.auth.Authenticate(ts.db.DB, username, password)
	if err != nil {
		t.Fatalf("failed to authenticate admin user: %v", err)
	}
	return session
}
