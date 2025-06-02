package server

import (
	"io"
	"log"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"infoscope/internal/auth"
	"infoscope/internal/database"
	"infoscope/internal/favicon"
	"infoscope/internal/feed"

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

// TODO: HTTP handler tests are currently disabled because Server doesn't expose its HTTP handler
// The Server type needs to either implement http.Handler or provide a method to access the router

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
