package server

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"
)

// gzipMiddleware compresses responses for clients that advertise gzip support.
// Static assets are excluded because they are served efficiently by the file server
// and may already be compressed by the browser cache policies.
func gzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip compression for static files or clients that do not accept gzip
		if strings.HasPrefix(r.URL.Path, "/static/") || !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") || r.Method == http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}

		// Avoid double-encoding
		if ce := w.Header().Get("Content-Encoding"); ce != "" {
			next.ServeHTTP(w, r)
			return
		}

		gzrw := newGzipResponseWriter(w)
		defer gzrw.Close()
		next.ServeHTTP(gzrw, r)
	})
}

type gzipResponseWriter struct {
	http.ResponseWriter
	wroteHeader bool
	gz          *gzip.Writer
	writer      io.Writer
}

func newGzipResponseWriter(w http.ResponseWriter) *gzipResponseWriter {
	return &gzipResponseWriter{ResponseWriter: w}
}

func (g *gzipResponseWriter) WriteHeader(statusCode int) {
	if g.wroteHeader {
		return
	}
	g.wroteHeader = true
	// Set compression headers
	g.Header().Set("Content-Encoding", "gzip")
	g.Header().Add("Vary", "Accept-Encoding")
	g.Header().Del("Content-Length")
	// Initialize gzip writer on first header write
	g.gz = gzip.NewWriter(g.ResponseWriter)
	g.writer = g.gz
	g.ResponseWriter.WriteHeader(statusCode)
}

func (g *gzipResponseWriter) Write(b []byte) (int, error) {
	if !g.wroteHeader {
		g.WriteHeader(http.StatusOK)
	}
	return g.writer.Write(b)
}

func (g *gzipResponseWriter) Close() error {
	if g.gz != nil {
		return g.gz.Close()
	}
	return nil
}
