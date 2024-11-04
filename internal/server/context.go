// internal/server/context.go
package server

import (
	"context"
)

type contextKey string

const (
	contextKeyUserID       contextKey = "userID"
	contextKeyCSRFMeta     contextKey = "csrfMeta"
	contextKeyCSRFToken    contextKey = "csrfToken"
	contextKeyTemplateData contextKey = "templateData"
)

// Context helper functions
func getUserID(ctx context.Context) (int64, bool) {
	userID, ok := ctx.Value(contextKeyUserID).(int64)
	return userID, ok
}
