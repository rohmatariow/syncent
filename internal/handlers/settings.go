package handlers

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	crand "crypto/rand"
	"crypto/sha256"
	"golang.org/x/crypto/pbkdf2"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"vps-command/internal/services"

	"github.com/go-chi/chi/v5"
)

func VerifyRecaptcha(token, secretKey string) bool {
	if secretKey == "" || token == "" { return true }
	resp, err := http.PostForm("https://www.google.com/recaptcha/api/siteverify",
		url.Values{"secret": {secretKey}, "response": {token}})
	if err != nil { return false }
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result struct { Success bool `json:"success"`; Score float64 `json:"score"` }
	json.Unmarshal(body, &result)
	return result.Success && result.Score >= 0.5
}

type captchaEntry struct { Answer string; ExpiresAt time.Time }
var (captchas = map[string]captchaEntry{}; captchaMu sync.Mutex)

func (app *App) GenerateCaptcha(w http.ResponseWriter, r *http.Request) {
	a, b := randInt(10, 50), randInt(1, 20)
	id := randomHex(16)
	captchaMu.Lock()
	captchas[id] = captchaEntry{Answer: fmt.Sprintf("%d", a+b), ExpiresAt: time.Now().Add(5 * time.Minute)}
	for k, v := range captchas { if time.Now().After(v.ExpiresAt) { delete(captchas, k) } }
	captchaMu.Unlock()
	respondJSON(w, 200, map[string]interface{}{"captcha_id": id, "question": fmt.Sprintf("%d + %d = ?", a, b)})
}

func VerifyCaptcha(id, answer string) bool {
	captchaMu.Lock(); defer captchaMu.Unlock()
	e, ok := captchas[id]; if !ok || time.Now().After(e.ExpiresAt) { delete(captchas, id); return false }
	delete(captchas, id); return e.Answer == answer
}

func randInt(min, max int) int { b := make([]byte, 1); crand.Read(b); return min + int(b[0])%(max-min+1) }
func randomHex(n int) string { b := make([]byte, n); crand.Read(b); return hex.EncodeToString(b) }

func (app *App) ValidateCredentials(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &body); err != nil || body.Username == "" || body.Password == "" {
		respondError(w, 400, "INVALID_INPUT", "Username and password required.")
		return
	}

	var hash string
	var fails int
	var lockedUntil *time.Time
	err := app.Pool.QueryRow(r.Context(),
		`SELECT password_hash, failed_login_attempts, locked_until FROM users WHERE username=$1`, body.Username,
	).Scan(&hash, &fails, &lockedUntil)

	if err != nil {
		services.DummyPasswordCheck()
		respondError(w, 401, "AUTH_FAILED", "Invalid username or password.")
		return
	}

	if lockedUntil != nil && time.Now().Before(*lockedUntil) {
		respondError(w, 429, "ACCOUNT_LOCKED", "Account locked. Try again later.")
		return
	}

	if !services.VerifyPassword(body.Password, hash) {
		newFails := fails + 1
		if newFails >= 5 {
			lockUntil := time.Now().Add(15 * time.Minute)
			app.Pool.Exec(r.Context(), `UPDATE users SET failed_login_attempts=$1, locked_until=$2 WHERE username=$3`, newFails, lockUntil, body.Username)
		} else {
			app.Pool.Exec(r.Context(), `UPDATE users SET failed_login_attempts=$1 WHERE username=$2`, newFails, body.Username)
		}
		respondError(w, 401, "AUTH_FAILED", "Invalid username or password.")
		return
	}
	respondJSON(w, 200, map[string]interface{}{"valid": true})
}

func (app *App) Register(w http.ResponseWriter, r *http.Request) {
	var allowed string
	app.Pool.Exec(r.Context(), `CREATE TABLE IF NOT EXISTS app_config (key VARCHAR(50) PRIMARY KEY, value TEXT)`)
	err := app.Pool.QueryRow(r.Context(), `SELECT value FROM app_config WHERE key='allow_registration'`).Scan(&allowed)
	if err != nil || allowed != "true" {
		respondError(w, 403, "REGISTRATION_DISABLED", "Registration is currently disabled.")
		return
	}

	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &body); err != nil || body.Username == "" || body.Password == "" {
		respondError(w, 400, "INVALID_INPUT", "Username and password required.")
		return
	}
	if len(body.Username) < 3 || len(body.Username) > 50 {
		respondError(w, 400, "INVALID_INPUT", "Username must be 3-50 characters.")
		return
	}
	if len(body.Password) < 12 {
		respondError(w, 400, "WEAK_PASSWORD", "Password must be at least 12 characters.")
		return
	}
	
	for _, c := range body.Username {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_') {
			respondError(w, 400, "INVALID_INPUT", "Username: letters, numbers, underscore only.")
			return
		}
	}

	var count int
	app.Pool.QueryRow(r.Context(), `SELECT COUNT(*) FROM users WHERE username=$1`, body.Username).Scan(&count)
	if count > 0 {
		respondError(w, 409, "USERNAME_TAKEN", "Username already exists.")
		return
	}

	hash, _ := services.HashPassword(body.Password)
	secret, uri, _ := services.GenerateTOTPSecret(body.Username)
	encSecret, _ := app.Crypto.Encrypt(secret)

	app.Pool.Exec(r.Context(),
		`INSERT INTO users (username, password_hash, totp_secret_encrypted) VALUES ($1, $2, $3)`,
		body.Username, hash, encSecret)

	services.WriteAuditLog(r.Context(), app.Pool, "user_registered", "New user: "+body.Username, nil, usernamePtr(r), getClientIP(r))

	w.Header().Set("Cache-Control", "no-store")
	respondJSON(w, 201, map[string]interface{}{
		"message":     "Account created. Set up 2FA to continue.",
		"totp_uri":    uri,
		"totp_secret": secret,
	})
}

func (app *App) IsRegistrationAllowed(w http.ResponseWriter, r *http.Request) {
	app.Pool.Exec(r.Context(), `CREATE TABLE IF NOT EXISTS app_config (key VARCHAR(50) PRIMARY KEY, value TEXT)`)
	var val string
	err := app.Pool.QueryRow(r.Context(), `SELECT value FROM app_config WHERE key='allow_registration'`).Scan(&val)
	respondJSON(w, 200, map[string]bool{"allowed": err == nil && val == "true"})
}

func (app *App) ResetPassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
		TOTPCode        string `json:"totp_code"`
	}
	if err := decodeJSON(r, &body); err != nil || body.NewPassword == "" || body.TOTPCode == "" || body.CurrentPassword == "" {
		respondError(w, 400, "INVALID_INPUT", "current_password, new_password, and totp_code required.")
		return
	}
	if len(body.NewPassword) < 12 {
		respondError(w, 400, "WEAK_PASSWORD", "New password must be at least 12 characters.")
		return
	}

	username := getUsername(r.Context())
	var hash, encTOTP string
	app.Pool.QueryRow(r.Context(), `SELECT password_hash, totp_secret_encrypted FROM users WHERE username=$1`, username).Scan(&hash, &encTOTP)
	if !services.VerifyPassword(body.CurrentPassword, hash) {
		respondError(w, 401, "AUTH_FAILED", "Current password is wrong.")
		return
	}
	secret, _ := app.Crypto.Decrypt(encTOTP)
	if !services.VerifyTOTP(secret, body.TOTPCode) {
		respondError(w, 403, "INVALID_TOTP", "Invalid TOTP code.")
		return
	}
	newHash, _ := services.HashPassword(body.NewPassword)
	app.Pool.Exec(r.Context(), `UPDATE users SET password_hash=$1, refresh_token_hash=NULL, updated_at=NOW() WHERE username=$2`, newHash, username)
	services.WriteAuditLog(r.Context(), app.Pool, "password_changed", "Password changed for "+username, nil, usernamePtr(r), getClientIP(r))
	respondJSON(w, 200, map[string]string{"message": "Password changed. All sessions revoked — please login again."})
}

func (app *App) GetAppConfig(w http.ResponseWriter, r *http.Request) {
	app.Pool.Exec(r.Context(), `CREATE TABLE IF NOT EXISTS app_config (key VARCHAR(50) PRIMARY KEY, value TEXT)`)
	rows, _ := app.Pool.Query(r.Context(), `SELECT key, value FROM app_config`)
	defer rows.Close()
	cfg := map[string]string{"allow_registration": "false", "max_servers": "50", "health_check_interval_default": "30", "log_retention_days": "30"}
	for rows.Next() { var k, v string; rows.Scan(&k, &v); cfg[k] = v }
	respondJSON(w, 200, cfg)
}

func (app *App) UpdateAppConfig(w http.ResponseWriter, r *http.Request) {
	var body map[string]string
	if err := decodeJSON(r, &body); err != nil { respondError(w, 400, "INVALID_INPUT", "JSON object required."); return }
	app.Pool.Exec(r.Context(), `CREATE TABLE IF NOT EXISTS app_config (key VARCHAR(50) PRIMARY KEY, value TEXT)`)
	allowed := map[string]bool{"allow_registration": true, "max_servers": true, "health_check_interval_default": true, "log_retention_days": true}
	for k, v := range body {
		if !allowed[k] { continue }
		app.Pool.Exec(r.Context(), `INSERT INTO app_config (key, value) VALUES ($1, $2) ON CONFLICT (key) DO UPDATE SET value=$2`, k, v)
	}
	services.WriteAuditLog(r.Context(), app.Pool, "config_changed", "App config updated", nil, usernamePtr(r), getClientIP(r))
	respondJSON(w, 200, map[string]string{"message": "Config updated."})
}

func (app *App) ExportConfig(w http.ResponseWriter, r *http.Request) {
	var body struct {
		WithKeys       bool   `json:"with_keys"`
		TOTPCode       string `json:"totp_code"`
		ExportPassword string `json:"export_password"`
	}
	decodeJSON(r, &body)
	withKeys := body.WithKeys
	totpCode := body.TOTPCode
	exportPassword := body.ExportPassword

	if withKeys {
		if totpCode == "" || exportPassword == "" {
			respondError(w, 400, "INVALID_INPUT", "with_keys requires totp_code and export_password params.")
			return
		}
		username := getUsername(r.Context())
		if !app.verifyUserTOTP(r.Context(), username, totpCode) {
			respondError(w, 403, "INVALID_TOTP", "Invalid TOTP code.")
			return
		}
	}

	username := getUsername(r.Context())
	var userID string
	app.Pool.QueryRow(r.Context(), `SELECT id FROM users WHERE username=$1`, username).Scan(&userID)

	rows, _ := app.Pool.Query(r.Context(),
		`SELECT name, ip_address, ssh_port, ssh_username, connection_mode, location, tags, notes, expected_services, log_paths, health_check_interval, encrypted_private_key, public_key, gsocket_secret
		 FROM servers WHERE user_id=$1 ORDER BY name`, userID)
	defer rows.Close()

	var servers []map[string]interface{}
	for rows.Next() {
		var name, ip, uname, mode string
		var port, hi int
		var loc, notes, encKey, pubKey, gsSecret *string
		var tagsJ, svcJ, logsJ json.RawMessage
		rows.Scan(&name, &ip, &port, &uname, &mode, &loc, &tagsJ, &notes, &svcJ, &logsJ, &hi, &encKey, &pubKey, &gsSecret)

		s := map[string]interface{}{
			"name": name, "ip_address": ip, "ssh_port": port, "ssh_username": uname,
			"connection_mode": mode, "location": loc, "tags": tagsJ, "notes": notes,
			"expected_services": svcJ, "log_paths": logsJ, "health_check_interval": hi,
		}

		if withKeys {
			if mode == "gsocket" && gsSecret != nil && *gsSecret != "" {
				encGs, err := exportEncrypt(*gsSecret, exportPassword)
				if err == nil { s["encrypted_gsocket_secret"] = encGs }
			} else if encKey != nil && *encKey != "" {
				plainKey, err := app.Crypto.Decrypt(*encKey)
				if err == nil {
					encExport, encErr := exportEncrypt(plainKey, exportPassword)
					if encErr == nil { s["encrypted_key"] = encExport }
					if pubKey != nil { s["public_key"] = *pubKey }
				}
			}
		}
		servers = append(servers, s)
	}
	if servers == nil { servers = []map[string]interface{}{} }

	services.WriteAuditLog(r.Context(), app.Pool, "config_exported",
		fmt.Sprintf("%d servers exported (keys: %v)", len(servers), withKeys), nil, usernamePtr(r), getClientIP(r))

	respondJSON(w, 200, map[string]interface{}{
		"version": "1.2.0", "exported": time.Now().UTC(), "count": len(servers),
		"with_keys": withKeys, "servers": servers,
		"note": func() string { if withKeys { return "Keys included (encrypted with export password)." }; return "Keys NOT included. Re-inject after import." }(),
	})
}

func (app *App) ImportConfig(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Servers        []map[string]interface{} `json:"servers"`
		WithKeys       bool                     `json:"with_keys"`
		ExportPassword string                   `json:"export_password"`
	}
	if err := decodeJSON(r, &body); err != nil || len(body.Servers) == 0 {
		respondError(w, 400, "INVALID_INPUT", "Valid servers array required.")
		return
	}

	username := getUsername(r.Context())
	var userID string
	app.Pool.QueryRow(r.Context(), `SELECT id FROM users WHERE username=$1`, username).Scan(&userID)

	imported := 0
	skippedKeys := 0
	for _, s := range body.Servers {
		name, _ := s["name"].(string)
		ip, _ := s["ip_address"].(string)
		uname, _ := s["ssh_username"].(string)
		if name == "" { continue }

		port := 22
		if p, ok := s["ssh_port"].(float64); ok { port = int(p) }
		mode := "direct"
		if m, ok := s["connection_mode"].(string); ok && m != "" { mode = m }
		hi := 30
		if h, ok := s["health_check_interval"].(float64); ok { hi = int(h) }

		if mode == "gsocket" {
			if ip == "" { ip = "gsocket" }
			if uname == "" { uname = "gsocket" }
		} else if ip == "" || uname == "" {
			continue
		}

		var encPriv, pubSSH string
		var gsSecret *string
		status := "pending_setup"

		if mode == "gsocket" && body.WithKeys {
			encGs, hasGs := s["encrypted_gsocket_secret"].(string)
			if hasGs && encGs != "" && body.ExportPassword != "" {
				plainGs, decErr := exportDecrypt(encGs, body.ExportPassword)
				if decErr == nil && plainGs != "" {
					gsSecret = &plainGs
				} else {
					skippedKeys++
					continue
				}
			} else if gs, ok := s["gsocket_secret"].(string); ok && gs != "" {
				gsSecret = &gs
			} else {
				skippedKeys++
				continue
			}
		} else if mode == "gsocket" && !body.WithKeys {
			if gs, ok := s["gsocket_secret"].(string); ok && gs != "" {
				gsSecret = &gs
			} else {
				continue
			}
		} else if body.WithKeys {
			encKeyStr, hasKey := s["encrypted_key"].(string)
			if hasKey && encKeyStr != "" && body.ExportPassword != "" {
				plainKey, decErr := exportDecrypt(encKeyStr, body.ExportPassword)
				if decErr == nil && strings.HasPrefix(plainKey, "-----BEGIN") {
					enc, err := app.Crypto.Encrypt(plainKey)
					if err == nil {
						encPriv = enc
						if pk, ok := s["public_key"].(string); ok { pubSSH = pk }
					}
				}
			}
			if encPriv == "" {
				skippedKeys++
				continue
			}
		} else {
			priv, pub, err := app.Crypto.GenerateEd25519Keypair()
			if err != nil { continue }
			enc, err := app.Crypto.Encrypt(priv)
			if err != nil { continue }
			encPriv = enc
			pubSSH = pub
		}

		tagsJ, _ := json.Marshal(s["tags"])
		svcJ, _ := json.Marshal(s["expected_services"])
		logsJ, _ := json.Marshal(s["log_paths"])
		loc, _ := s["location"].(string)
		notes, _ := s["notes"].(string)

		_, err := app.Pool.Exec(r.Context(),
			`INSERT INTO servers (name, ip_address, ssh_port, ssh_username, connection_mode,
			 encrypted_private_key, public_key, location, tags, notes, expected_services,
			 log_paths, health_check_interval, status, user_id, gsocket_secret)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`,
			name, ip, port, uname, mode, encPriv, pubSSH, loc, tagsJ, notes, svcJ, logsJ, hi, status, userID, gsSecret)
		if err == nil { imported++ }
	}

	services.WriteAuditLog(r.Context(), app.Pool, "config_imported",
		fmt.Sprintf("%d servers imported, %d key decryption failed", imported, skippedKeys), nil, usernamePtr(r), getClientIP(r))

	resp := map[string]interface{}{
		"message": fmt.Sprintf("%d servers imported.", imported), "imported": imported,
	}
	if skippedKeys > 0 {
		resp["message"] = fmt.Sprintf("%d servers imported, %d skipped (wrong export password).", imported, skippedKeys)
		resp["skipped"] = skippedKeys
		resp["note"] = "Some servers were skipped because the export password was incorrect. Keys could not be decrypted."
	} else if !body.WithKeys {
		resp["note"] = "New SSH keys generated. Use 'Re-inject Key' or 'Inject Key' on each server to activate."
	} else {
		resp["note"] = "Keys imported. Test connection on each server to verify."
	}
	respondJSON(w, 200, resp)
}

func (app *App) ReinjectKey(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	userID := app.getUserID(r.Context(), getUsername(r.Context()))
	var name, pubKey string
	err := app.Pool.QueryRow(r.Context(), `SELECT name, public_key FROM servers WHERE id=$1 AND user_id=$2`, id, userID).Scan(&name, &pubKey)
	if err != nil {
		respondError(w, 404, "NOT_FOUND", "Server not found.")
		return
	}
	respondJSON(w, 200, map[string]interface{}{
		"server_name": name, "public_key": pubKey,
		"message": "Copy the public key to the server, or use inject-key with SSH password.",
		"manual_command": fmt.Sprintf("echo '%s' >> ~/.ssh/authorized_keys", pubKey),
	})
}

func exportEncrypt(plaintext, password string) (string, error) {
	key := deriveKey(password)
	block, err := aes.NewCipher(key)
	if err != nil { return "", err }
	gcm, err := cipher.NewGCM(block)
	if err != nil { return "", err }
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(crand.Reader, nonce); err != nil { return "", err }
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return hex.EncodeToString(ciphertext), nil
}

func exportDecrypt(hexData, password string) (string, error) {
	data, err := hex.DecodeString(hexData)
	if err != nil { return "", fmt.Errorf("invalid hex data") }
	key := deriveKey(password)
	block, err := aes.NewCipher(key)
	if err != nil { return "", err }
	gcm, err := cipher.NewGCM(block)
	if err != nil { return "", err }
	if len(data) < gcm.NonceSize() { return "", fmt.Errorf("data too short") }
	nonce, ciphertext := data[:gcm.NonceSize()], data[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil { return "", fmt.Errorf("wrong password or corrupted data") }
	return string(plaintext), nil
}

func deriveKey(password string) []byte {
	salt := []byte("syncent-export-v1")
	return pbkdf2.Key([]byte(password), salt, 100000, 32, sha256.New)
}

func (app *App) is2FARequired() bool {
	var val string
	app.Pool.QueryRow(context.Background(), `SELECT value FROM app_config WHERE key='require_2fa'`).Scan(&val)
	return val != "false"
}

func (app *App) Get2FAStatus(w http.ResponseWriter, r *http.Request) {
	app.Pool.Exec(r.Context(), `CREATE TABLE IF NOT EXISTS app_config (key VARCHAR(50) PRIMARY KEY, value TEXT)`)
	respondJSON(w, 200, map[string]interface{}{"required": app.is2FARequired()})
}

func (app *App) Toggle2FA(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enable       bool   `json:"enable"`
		TOTPCode     string `json:"totp_code"`
		Confirmation string `json:"confirmation"`
	}
	if err := decodeJSON(r, &body); err != nil {
		respondError(w, 400, "INVALID_INPUT", "Invalid request.")
		return
	}

	username := getUsername(r.Context())
	currently := app.is2FARequired()

	if body.Enable && !currently {
		if body.Confirmation != "ENABLE 2FA" {
			respondError(w, 400, "CONFIRM_REQUIRED", "Type 'ENABLE 2FA' to confirm.")
			return
		}
		app.Pool.Exec(r.Context(), `INSERT INTO app_config (key, value) VALUES ('require_2fa', 'true') ON CONFLICT (key) DO UPDATE SET value='true'`)
		services.WriteAuditLog(r.Context(), app.Pool, "2fa_enabled", "2FA enabled by "+username, nil, usernamePtr(r), getClientIP(r))
		respondJSON(w, 200, map[string]string{"message": "2FA has been enabled."})
		return
	}

	if !body.Enable && currently {
		if body.TOTPCode == "" {
			respondError(w, 400, "TOTP_REQUIRED", "TOTP code required to disable 2FA.")
			return
		}
		if body.Confirmation != "DISABLE 2FA" {
			respondError(w, 400, "CONFIRM_REQUIRED", "Type 'DISABLE 2FA' to confirm.")
			return
		}
		if !app.verifyUserTOTP(r.Context(), username, body.TOTPCode) {
			respondError(w, 403, "INVALID_TOTP", "Invalid TOTP code.")
			return
		}
		app.Pool.Exec(r.Context(), `INSERT INTO app_config (key, value) VALUES ('require_2fa', 'false') ON CONFLICT (key) DO UPDATE SET value='false'`)
		services.WriteAuditLog(r.Context(), app.Pool, "2fa_disabled", "2FA disabled by "+username, nil, usernamePtr(r), getClientIP(r))
		respondJSON(w, 200, map[string]string{"message": "2FA has been disabled. Verification will use text confirmation instead."})
		return
	}

	respondJSON(w, 200, map[string]string{"message": "No change."})
}

func (app *App) VerifyAction(ctx context.Context, username, totpCode, confirmation, confirmText string) bool {
	if app.is2FARequired() {
		return app.verifyUserTOTP(ctx, username, totpCode)
	}
	return confirmation == confirmText
}
