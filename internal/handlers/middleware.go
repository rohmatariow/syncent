package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"vps-command/internal/config"
	"vps-command/internal/services"

	"github.com/jackc/pgx/v5/pgxpool"
)

type App struct {
	Config *config.Config
	Pool   *pgxpool.Pool
	Crypto *services.CryptoService
	SSH    *services.SSHManager
	Health *services.HealthChecker
}

func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func respondError(w http.ResponseWriter, status int, code, message string, suggestion ...string) {
	resp := map[string]interface{}{"error": true, "code": code, "message": message}
	if len(suggestion) > 0 { resp["suggestion"] = suggestion[0] }
	respondJSON(w, status, resp)
}

func decodeJSON(r *http.Request, v interface{}) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 10*1024*1024)
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

func getClientIP(r *http.Request) string {
	if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
		return strings.TrimSpace(realIP)
	}
	host := r.RemoteAddr
	if idx := strings.LastIndex(host, ":"); idx != -1 {
		host = host[:idx]
	}
	return strings.Trim(host, "[]")
}

func (app *App) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
			respondError(w, 401, "NO_TOKEN", "Missing authorization token.")
			return
		}
		username, err := services.VerifyAccessToken(strings.TrimPrefix(auth, "Bearer "), app.Config.JWTSecret)
		if err != nil {
			respondError(w, 401, "TOKEN_INVALID", "Invalid or expired token.")
			return
		}
		next.ServeHTTP(w, r.WithContext(withUsername(r.Context(), username)))
	})
}

func (app *App) AdminMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username := getUsername(r.Context())
		var role string
		err := app.Pool.QueryRow(r.Context(),
			`SELECT COALESCE(role, 'user') FROM users WHERE username=$1`, username).Scan(&role)
		if err != nil || role != "admin" {
			respondError(w, 403, "FORBIDDEN", "Admin access required.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func SecurityHeaders(isDev bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("Referrer-Policy", "no-referrer")
			w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' https://cdnjs.cloudflare.com; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self' wss: ws:; font-src 'self'")
			w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
			if !isDev {
				w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (app *App) VerifyServerOwnership(ctx context.Context, username, serverID string) bool {
	var count int
	app.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM servers s JOIN users u ON s.user_id = u.id WHERE s.id=$1 AND u.username=$2`,
		serverID, username).Scan(&count)
	return count > 0
}
