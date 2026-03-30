package handlers

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"vps-command/internal/services"
)

func (app *App) CheckSetup(w http.ResponseWriter, r *http.Request) {
	var count int
	app.Pool.QueryRow(r.Context(), `SELECT COUNT(*) FROM users`).Scan(&count)
	respondJSON(w, 200, map[string]interface{}{"setup_required": count == 0})
}

func (app *App) Setup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &body); err != nil || body.Username == "" || body.Password == "" {
		respondError(w, 400, "INVALID_INPUT", "Username and password required.")
		return
	}
	if len(body.Password) < 12 {
		respondError(w, 400, "WEAK_PASSWORD", "Password must be at least 12 characters.")
		return
	}

	hash, err := services.HashPassword(body.Password)
	if err != nil { respondError(w, 500, "HASH_ERROR", "Failed to hash password."); return }

	secret, uri, err := services.GenerateTOTPSecret(body.Username)
	if err != nil { respondError(w, 500, "TOTP_ERROR", "Failed to generate TOTP."); return }

	encSecret, err := app.Crypto.Encrypt(secret)
	if err != nil { respondError(w, 500, "ENCRYPT_ERROR", "Failed to encrypt TOTP secret."); return }

	tag, err := app.Pool.Exec(r.Context(),
		`INSERT INTO users (username, password_hash, totp_secret_encrypted, role)
		 SELECT $1, $2, $3, 'admin' WHERE NOT EXISTS (SELECT 1 FROM users)`,
		body.Username, hash, encSecret)
	if err != nil || tag.RowsAffected() == 0 {
		respondError(w, 403, "SETUP_DONE", "Admin already exists.")
		return
	}

	services.WriteAuditLog(r.Context(), app.Pool, "admin_setup", "Admin created: "+body.Username, nil, nil, getClientIP(r))

	w.Header().Set("Cache-Control", "no-store")
	respondJSON(w, 201, map[string]interface{}{
		"message":     "Admin created. Scan QR or enter secret in authenticator app.",
		"totp_uri":    uri,
		"totp_secret": secret,
	})
}

func (app *App) VerifyTOTPSetup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		TOTPCode string `json:"totp_code"`
	}
	if err := decodeJSON(r, &body); err != nil {
		respondError(w, 400, "INVALID_INPUT", "Username and TOTP code required.")
		return
	}

	var encSecret string
	var enabled bool
	err := app.Pool.QueryRow(r.Context(),
		`SELECT totp_secret_encrypted, is_totp_enabled FROM users WHERE username=$1`, body.Username,
	).Scan(&encSecret, &enabled)
	if err != nil { respondError(w, 404, "USER_NOT_FOUND", "User not found."); return }
	if enabled { respondError(w, 400, "TOTP_ALREADY_ENABLED", "2FA already enabled."); return }

	secret, err := app.Crypto.Decrypt(encSecret)
	if err != nil { respondError(w, 500, "DECRYPT_ERROR", "Internal error."); return }

	if !services.VerifyTOTP(secret, body.TOTPCode) {
		respondError(w, 400, "INVALID_TOTP", "Invalid code. Check authenticator app time sync.")
		return
	}

	app.Pool.Exec(r.Context(), `UPDATE users SET is_totp_enabled=true WHERE username=$1`, body.Username)
	services.WriteAuditLog(r.Context(), app.Pool, "totp_enabled", "2FA enabled for "+body.Username, nil, nil, getClientIP(r))
	respondJSON(w, 200, map[string]string{"message": "2FA verified and enabled. You can now login."})
}

func (app *App) Login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
		TOTPCode string `json:"totp_code"`
	}
	if err := decodeJSON(r, &body); err != nil {
		respondError(w, 400, "INVALID_INPUT", "Username, password, and TOTP code required.")
		return
	}

	ctx := r.Context()
	ip := getClientIP(r)

	var id, hash, encTOTP string
	var enabled bool
	var fails int
	var lockedUntil *time.Time

	err := app.Pool.QueryRow(ctx,
		`SELECT id, password_hash, totp_secret_encrypted, is_totp_enabled, failed_login_attempts, locked_until
		 FROM users WHERE username=$1`, body.Username,
	).Scan(&id, &hash, &encTOTP, &enabled, &fails, &lockedUntil)

	if err != nil {
		services.DummyPasswordCheck()
		respondError(w, 401, "AUTH_FAILED", "Invalid credentials.")
		return
	}

	if lockedUntil != nil && time.Now().Before(*lockedUntil) {
		remaining := int(time.Until(*lockedUntil).Seconds())
		respondError(w, 429, "ACCOUNT_LOCKED", fmt.Sprintf("Account locked. Try again in %d seconds.", remaining))
		return
	}

	if !services.VerifyPassword(body.Password, hash) {
		app.incrementFailures(ctx, body.Username, fails+1, ip)
		respondError(w, 401, "AUTH_FAILED", "Invalid credentials.")
		return
	}

	if !enabled {
		respondError(w, 403, "TOTP_NOT_SETUP", "2FA not configured yet.")
		return
	}

	if body.TOTPCode == "" {
		respondError(w, 400, "TOTP_REQUIRED", "TOTP code required.")
		return
	}
	secret, err := app.Crypto.Decrypt(encTOTP)
	if err != nil { respondError(w, 500, "DECRYPT_ERROR", "Internal error."); return }
	if !services.VerifyTOTPWithReplay(ctx, app.Pool, body.Username, secret, body.TOTPCode) {
		app.incrementFailures(ctx, body.Username, fails+1, ip)
		respondError(w, 401, "AUTH_FAILED", "Invalid credentials.")
		return
	}

	app.Pool.Exec(ctx, `UPDATE users SET failed_login_attempts=0, locked_until=NULL WHERE username=$1`, body.Username)

	accessToken, expiresIn, _ := services.CreateAccessToken(body.Username, app.Config.JWTSecret, app.Config.JWTAccessExpireMin)
	refreshToken, refreshHash, _ := services.CreateRefreshToken(body.Username, app.Config.JWTSecret, app.Config.JWTRefreshExpireDays)

	app.Pool.Exec(ctx, `UPDATE users SET refresh_token_hash=$1 WHERE username=$2`, refreshHash, body.Username)

	http.SetCookie(w, &http.Cookie{
		Name: "refresh_token", Value: refreshToken, Path: "/api/auth",
		HttpOnly: true, Secure: app.Config.EnforceHTTPS,
		SameSite: http.SameSiteStrictMode, MaxAge: app.Config.JWTRefreshExpireDays * 86400,
	})

	services.WriteAuditLog(ctx, app.Pool, "login_success", "Login: "+body.Username, nil, nil, ip)

	respondJSON(w, 200, map[string]interface{}{
		"access_token": accessToken, "token_type": "bearer", "expires_in": expiresIn,
	})
}

func (app *App) Refresh(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("refresh_token")
	if err != nil { respondError(w, 401, "NO_REFRESH", "No refresh token."); return }

	username, tokenHash, err := services.VerifyRefreshToken(cookie.Value, app.Config.JWTSecret)
	if err != nil { respondError(w, 401, "REFRESH_INVALID", "Invalid refresh token."); return }

	var storedHash *string
	app.Pool.QueryRow(r.Context(), `SELECT refresh_token_hash FROM users WHERE username=$1`, username).Scan(&storedHash)
	if storedHash == nil || *storedHash != tokenHash {
		respondError(w, 401, "REFRESH_REVOKED", "Token revoked."); return
	}

	accessToken, expiresIn, _ := services.CreateAccessToken(username, app.Config.JWTSecret, app.Config.JWTAccessExpireMin)
	respondJSON(w, 200, map[string]interface{}{
		"access_token": accessToken, "token_type": "bearer", "expires_in": expiresIn,
	})
}

func (app *App) Logout(w http.ResponseWriter, r *http.Request) {
	username := getUsername(r.Context())
	app.Pool.Exec(r.Context(), `UPDATE users SET refresh_token_hash=NULL WHERE username=$1`, username)
	http.SetCookie(w, &http.Cookie{
		Name: "refresh_token", Value: "", Path: "/api/auth", HttpOnly: true, MaxAge: -1,
	})
	services.WriteAuditLog(r.Context(), app.Pool, "logout", "Logout: "+username, nil, nil, getClientIP(r))
	respondJSON(w, 200, map[string]string{"message": "Logged out."})
}

func (app *App) Me(w http.ResponseWriter, r *http.Request) {
	username := getUsername(r.Context())
	var role string
	app.Pool.QueryRow(r.Context(), `SELECT COALESCE(role,'user') FROM users WHERE username=$1`, username).Scan(&role)
	respondJSON(w, 200, map[string]interface{}{"username": username, "role": role})
}

func (app *App) incrementFailures(ctx context.Context, username string, newCount int, ip string) {
	if newCount >= 5 {
		lockUntil := time.Now().Add(15 * time.Minute)
		app.Pool.Exec(ctx, `UPDATE users SET failed_login_attempts=$1, locked_until=$2 WHERE username=$3`,
			newCount, lockUntil, username)
		services.WriteAuditLog(ctx, app.Pool, "login_locked",
			fmt.Sprintf("Locked after %d fails: %s", newCount, username), nil, &username, ip)
	} else {
		app.Pool.Exec(ctx, `UPDATE users SET failed_login_attempts=$1 WHERE username=$2`, newCount, username)
		services.WriteAuditLog(ctx, app.Pool, "login_failed", "Failed login: "+username, nil, &username, ip)
	}
}
