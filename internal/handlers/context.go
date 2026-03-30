package handlers

import (
	"context"
	"net/http"
)

type ctxKey int

const ctxUsername ctxKey = iota

func withUsername(ctx context.Context, username string) context.Context {
	return context.WithValue(ctx, ctxUsername, username)
}

func getUsername(ctx context.Context) string {
	if v, ok := ctx.Value(ctxUsername).(string); ok {
		return v
	}
	return ""
}

func RespondPublicJSON(w http.ResponseWriter, status int, data interface{}) {
	respondJSON(w, status, data)
}
