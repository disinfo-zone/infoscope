package server

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

var cssSizePattern = regexp.MustCompile(`^\d+(\.\d+)?(px|em|rem|%|vh|vw|vmin|vmax)$`)

func sanitizeCSSSize(value string) string {
	v := strings.TrimSpace(value)
	if v == "" {
		return ""
	}
	if v == "0" || v == "auto" {
		return v
	}
	if cssSizePattern.MatchString(v) {
		return v
	}
	return ""
}

// handleRuntimeCSS serves dynamic CSS variables derived from settings.
func (s *Server) handleRuntimeCSS(w http.ResponseWriter, r *http.Request) {
	settings, err := s.getSettings(r.Context())
	if err != nil {
		s.logger.Printf("Error getting settings for runtime CSS: %v", err)
	}

	footerHeight := ""
	if settings != nil {
		footerHeight = settings["footer_image_height"]
	}
	footerHeight = sanitizeCSSSize(footerHeight)
	if footerHeight == "" {
		footerHeight = "auto"
	}

	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprintf(w, ":root{--footer-image-height:%s;}", footerHeight)
}
