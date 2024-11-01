// Save as: internal/server/context.go

package server

import (
	"context"
	"html/template"
)

type contextKey string

const (
	contextKeyUserID    contextKey = "userID"
	contextKeyCSRFMeta  contextKey = "csrfMeta"
	contextKeyCSRFToken contextKey = "csrfToken"
)

// getUserID retrieves the user ID from the context
func getUserID(ctx context.Context) (int64, bool) {
	userID, ok := ctx.Value(contextKeyUserID).(int64)
	return userID, ok
}

// getCSRFMeta retrieves the CSRF meta from the context
func getCSRFMeta(ctx context.Context) (template.HTML, bool) {
	csrfMeta, ok := ctx.Value(contextKeyCSRFMeta).(template.HTML)
	return csrfMeta, ok
}
