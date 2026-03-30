package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"vps-command/internal/services"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/ssh"
)

func usernamePtr(r *http.Request) *string {
	u := getUsername(r.Context())
	if u == "" { return nil }
	return &u
}

func (app *App) getUserID(ctx context.Context, username string) string {
	var id string
	app.Pool.QueryRow(ctx, `SELECT id FROM users WHERE username=$1`, username).Scan(&id)
	return id
}

func (app *App) CreateServer(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name           string   `json:"name"`
		IP             string   `json:"ip_address"`
		Port           int      `json:"ssh_port"`
		Username       string   `json:"ssh_username"`
		ConnMode       string   `json:"connection_mode"`
		GSocketSecret  string   `json:"gsocket_secret"`
		Location       string   `json:"location"`
		Notes          string   `json:"notes"`
		Tags           []string `json:"tags"`
		ExpServices    []string `json:"expected_services"`
		LogPaths       []string `json:"log_paths"`
		HealthInt      int      `json:"health_check_interval"`
	}
	if err := decodeJSON(r, &body); err != nil || body.Name == "" {
		respondError(w, 400, "INVALID_INPUT", "Name required.")
		return
	}
	if body.ConnMode == "gsocket" {
		if body.GSocketSecret == "" {
			respondError(w, 400, "INVALID_INPUT", "GSSocket secret required.")
			return
		}
		if body.Username == "" { body.Username = "gsocket" }
		if body.IP == "" { body.IP = "gsocket" }
	} else {
		if body.IP == "" || body.Username == "" {
			respondError(w, 400, "INVALID_INPUT", "IP address and SSH username required for direct connection.")
			return
		}
	}
	if body.Port == 0 { body.Port = 22 }
	if body.ConnMode == "" { body.ConnMode = "direct" }
	if body.HealthInt == 0 { body.HealthInt = 30 }
	if len(body.LogPaths) == 0 { body.LogPaths = []string{"/var/log/syslog"} }
	if body.Tags == nil { body.Tags = []string{} }
	if body.ExpServices == nil { body.ExpServices = []string{} }

	userID := app.getUserID(r.Context(), getUsername(r.Context()))

	var encPriv, pubSSH string
	if body.ConnMode != "gsocket" {
		privPEM, pub, err := app.Crypto.GenerateEd25519Keypair()
		if err != nil { respondError(w, 500, "KEY_GEN_ERROR", "Key generation failed."); return }
		encPriv, _ = app.Crypto.Encrypt(privPEM)
		pubSSH = pub
	}
	tagsJSON, _ := json.Marshal(body.Tags)
	svcJSON, _ := json.Marshal(body.ExpServices)
	logsJSON, _ := json.Marshal(body.LogPaths)

	var gsocketSecret *string
	if body.ConnMode == "gsocket" {
		encGs, err := app.Crypto.Encrypt(body.GSocketSecret)
		if err != nil { respondError(w, 500, "ENCRYPT_ERROR", "Failed to encrypt secret."); return }
		gsocketSecret = &encGs
	}

	var serverID uuid.UUID
	err := app.Pool.QueryRow(r.Context(),
		`INSERT INTO servers (user_id, name, ip_address, ssh_port, ssh_username, connection_mode,
		 encrypted_private_key, public_key, location, tags, notes, expected_services,
		 log_paths, health_check_interval, status, gsocket_secret)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,'pending_setup',$15) RETURNING id`,
		userID, body.Name, body.IP, body.Port, body.Username, body.ConnMode,
		encPriv, pubSSH, body.Location, tagsJSON, body.Notes, svcJSON, logsJSON, body.HealthInt, gsocketSecret,
	).Scan(&serverID)
	if err != nil { respondError(w, 500, "DB_ERROR", "Server creation failed."); return }

	services.WriteAuditLog(r.Context(), app.Pool, "server_added",
		fmt.Sprintf("Added: %s (mode=%s)", body.Name, body.ConnMode), &serverID, usernamePtr(r), getClientIP(r))

	resp := map[string]interface{}{
		"id": serverID, "name": body.Name, "connection_mode": body.ConnMode,
	}
	if body.ConnMode == "gsocket" {
		resp["message"] = "GSSocket server added. Click Test Connection to verify."
	} else {
		resp["public_key"] = pubSSH
		resp["message"] = fmt.Sprintf("echo '%s' >> ~/.ssh/authorized_keys", pubSSH)
	}
	respondJSON(w, 201, resp)
}

func (app *App) ListServers(w http.ResponseWriter, r *http.Request) {
	userID := app.getUserID(r.Context(), getUsername(r.Context()))
	rows, _ := app.Pool.Query(r.Context(),
		`SELECT id, name, ip_address, ssh_port, status, location, os_info, connection_mode, tags, last_ping_ms, last_checked_at
		 FROM servers WHERE user_id=$1 ORDER BY name`, userID)
	defer rows.Close()
	var servers []map[string]interface{}
	for rows.Next() {
		var id uuid.UUID; var name, ip, status, connMode string; var port int
		var loc, osInfo *string; var tags json.RawMessage; var ping *float64; var checked *time.Time
		rows.Scan(&id, &name, &ip, &port, &status, &loc, &osInfo, &connMode, &tags, &ping, &checked)
		servers = append(servers, map[string]interface{}{
			"id": id, "name": name, "ip_address": ip, "ssh_port": port,
			"status": status, "location": loc, "os_info": osInfo, "connection_mode": connMode,
			"tags": tags, "last_ping_ms": ping, "last_checked_at": checked,
		})
	}
	if servers == nil { servers = []map[string]interface{}{} }
	respondJSON(w, 200, servers)
}

func (app *App) GetServer(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	userID := app.getUserID(r.Context(), getUsername(r.Context()))

	var srvID, name, ip, username, connMode, status string
	var port, healthInt int
	var pubKey, location, osInfo, notes, kernelInfo *string
	var tags, expSvc, logPaths json.RawMessage
	var pingMs *float64
	var checkedAt, createdAt, updatedAt *time.Time

	err := app.Pool.QueryRow(r.Context(),
		`SELECT id, name, ip_address, ssh_port, ssh_username, connection_mode, public_key,
		 location, os_info, tags, notes, expected_services, status, last_ping_ms,
		 health_check_interval, log_paths, last_checked_at, created_at, updated_at, kernel_info
		 FROM servers WHERE id=$1 AND user_id=$2`, id, userID,
	).Scan(&srvID, &name, &ip, &port, &username, &connMode, &pubKey,
		&location, &osInfo, &tags, &notes, &expSvc, &status, &pingMs,
		&healthInt, &logPaths, &checkedAt, &createdAt, &updatedAt, &kernelInfo)
	if err != nil { respondError(w, 404, "NOT_FOUND", "Server not found."); return }

	respondJSON(w, 200, map[string]interface{}{
		"id": srvID, "name": name, "ip_address": ip, "ssh_port": port,
		"ssh_username": username, "connection_mode": connMode, "public_key": pubKey,
		"location": location, "os_info": osInfo, "kernel_info": kernelInfo,
		"tags": tags, "notes": notes,
		"expected_services": expSvc, "status": status, "last_ping_ms": pingMs,
		"health_check_interval": healthInt, "log_paths": logPaths,
		"last_checked_at": checkedAt, "created_at": createdAt, "updated_at": updatedAt,
	})
}

func (app *App) UpdateServer(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	userID := app.getUserID(r.Context(), getUsername(r.Context()))
	var body map[string]interface{}
	if err := decodeJSON(r, &body); err != nil { respondError(w, 400, "INVALID_INPUT", "Invalid JSON."); return }
	for key, val := range body {
		switch key {
		case "name":
			app.Pool.Exec(r.Context(), `UPDATE servers SET name=$1, updated_at=NOW() WHERE id=$2 AND user_id=$3`, val, id, userID)
		case "location":
			app.Pool.Exec(r.Context(), `UPDATE servers SET location=$1, updated_at=NOW() WHERE id=$2 AND user_id=$3`, val, id, userID)
		case "notes":
			app.Pool.Exec(r.Context(), `UPDATE servers SET notes=$1, updated_at=NOW() WHERE id=$2 AND user_id=$3`, val, id, userID)
		case "health_check_interval":
			app.Pool.Exec(r.Context(), `UPDATE servers SET health_check_interval=$1, updated_at=NOW() WHERE id=$2 AND user_id=$3`, val, id, userID)
		case "tags", "expected_services", "log_paths":
			j, _ := json.Marshal(val)
			switch key {
			case "tags":
				app.Pool.Exec(r.Context(), `UPDATE servers SET tags=$1, updated_at=NOW() WHERE id=$2 AND user_id=$3`, j, id, userID)
			case "expected_services":
				app.Pool.Exec(r.Context(), `UPDATE servers SET expected_services=$1, updated_at=NOW() WHERE id=$2 AND user_id=$3`, j, id, userID)
			case "log_paths":
				app.Pool.Exec(r.Context(), `UPDATE servers SET log_paths=$1, updated_at=NOW() WHERE id=$2 AND user_id=$3`, j, id, userID)
			}
		}
	}
	srvUUID, _ := uuid.Parse(id)
	services.WriteAuditLog(r.Context(), app.Pool, "server_updated", "Config updated", &srvUUID, usernamePtr(r), getClientIP(r))
	respondJSON(w, 200, map[string]string{"message": "Server updated."})
}

func (app *App) DeleteServer(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	userID := app.getUserID(r.Context(), getUsername(r.Context()))
	var body struct {
		TOTPCode     string `json:"totp_code"`
		Confirmation string `json:"confirmation"`
	}
	if err := decodeJSON(r, &body); err != nil {
		respondError(w, 400, "INVALID_INPUT", "Verification required."); return
	}
	if !app.VerifyAction(r.Context(), getUsername(r.Context()), body.TOTPCode, body.Confirmation, "DELETE") {
		respondError(w, 403, "VERIFY_FAILED", "Verification failed."); return
	}
	var name string
	app.Pool.QueryRow(r.Context(), `SELECT name FROM servers WHERE id=$1 AND user_id=$2`, id, userID).Scan(&name)
	app.Pool.Exec(r.Context(), `DELETE FROM servers WHERE id=$1 AND user_id=$2`, id, userID)
	services.WriteAuditLog(r.Context(), app.Pool, "server_removed", "Removed: "+name, nil, usernamePtr(r), getClientIP(r))
	respondJSON(w, 200, map[string]string{"message": "Server '" + name + "' removed."})
}

func (app *App) InjectKey(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	userID := app.getUserID(r.Context(), getUsername(r.Context()))
	var body struct{ SSHPassword string `json:"ssh_password"` }
	if err := decodeJSON(r, &body); err != nil || body.SSHPassword == "" {
		respondError(w, 400, "INVALID_INPUT", "SSH password required."); return
	}
	var ip, uname, pubKey, encKey, name string; var port int
	err := app.Pool.QueryRow(r.Context(),
		`SELECT ip_address, ssh_port, ssh_username, public_key, encrypted_private_key, name FROM servers WHERE id=$1 AND user_id=$2`, id, userID,
	).Scan(&ip, &port, &uname, &pubKey, &encKey, &name)
	if err != nil { respondError(w, 404, "NOT_FOUND", "Server not found."); return }

	client, err := app.SSH.ConnectWithPassword(ip, port, uname, body.SSHPassword)
	if err != nil {
		respondError(w, 502, "SSH_FAILED", "SSH authentication failed. Check your password.", "Ensure password authentication is enabled on the server.")
		return
	}
	defer client.Close()

	if !strings.HasPrefix(pubKey, "ssh-ed25519 ") || strings.ContainsAny(pubKey, "`$;|&\n\r") {
		respondError(w, 500, "KEY_FORMAT_ERROR", "Invalid public key format.")
		return
	}

	services.RunOnClient(client, "mkdir -p ~/.ssh && chmod 700 ~/.ssh")
	services.RunOnClient(client, "touch ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys")

	injectCmd := fmt.Sprintf(`grep -qF "%s" ~/.ssh/authorized_keys 2>/dev/null || printf '%%s\n' "%s" >> ~/.ssh/authorized_keys`,
		strings.ReplaceAll(pubKey, `"`, `\"`),
		strings.ReplaceAll(pubKey, `"`, `\"`))
	services.RunOnClient(client, injectCmd)

	osInfo, _ := services.RunOnClient(client, `cat /etc/os-release 2>/dev/null | grep PRETTY_NAME | cut -d'"' -f2 || uname -s`)
	kernel, _ := services.RunOnClient(client, `uname -r -v`)
	osInfo = strings.TrimSpace(osInfo)
	kernel = strings.TrimSpace(kernel)
	client.Close()

	signer, err := app.Crypto.ParsePrivateKey(encKey)
	if err != nil {
		app.Pool.Exec(r.Context(), `UPDATE servers SET status='auth_failed', os_info=$1, kernel_info=$2 WHERE id=$3`, osInfo, kernel, id)
		respondError(w, 500, "KEY_ERROR", "Key was injected but verification failed. Try manual key setup.")
		return
	}
	verifyClient, err := ssh.Dial("tcp", fmt.Sprintf("%s:%d", ip, port), &ssh.ClientConfig{
		User: uname, Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: 10 * time.Second,
	})
	if err != nil {
		app.Pool.Exec(r.Context(), `UPDATE servers SET status='auth_failed', os_info=$1, kernel_info=$2 WHERE id=$3`, osInfo, kernel, id)
		srvUUID, _ := uuid.Parse(id)
		services.WriteAuditLog(r.Context(), app.Pool, "key_inject_failed",
			fmt.Sprintf("Key injected but verification failed for %s: %v", name, err), &srvUUID, usernamePtr(r), getClientIP(r))
		respondError(w, 502, "KEY_VERIFY_FAILED",
			"Key was written to authorized_keys but SSH key authentication failed. Check file permissions: ~/.ssh (700), authorized_keys (600). Also verify the server allows PubkeyAuthentication.",
			"Try: ssh -i <key> "+uname+"@"+ip+" to debug manually.")
		return
	}
	verifyClient.Close()
	srvUUID, _ := uuid.Parse(id)
	app.Pool.Exec(r.Context(), `UPDATE servers SET status='online', os_info=$1, kernel_info=$2, last_checked_at=NOW() WHERE id=$3`, osInfo, kernel, id)
	services.WriteAuditLog(r.Context(), app.Pool, "key_injected", "Key injected and verified for "+name, &srvUUID, usernamePtr(r), getClientIP(r))
	respondJSON(w, 200, map[string]string{"message": "SSH key installed and verified on " + name + ". Password was not stored."})
}

func (app *App) ManualKeySetup(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	userID := app.getUserID(r.Context(), getUsername(r.Context()))
	var ip, uname, encKey, name, connMode string
	var port int
	var gsSecret *string
	err := app.Pool.QueryRow(r.Context(),
		`SELECT ip_address, ssh_port, ssh_username, encrypted_private_key, name, connection_mode, gsocket_secret
		 FROM servers WHERE id=$1 AND user_id=$2`, id, userID,
	).Scan(&ip, &port, &uname, &encKey, &name, &connMode, &gsSecret)
	if err != nil { respondError(w, 404, "NOT_FOUND", "Server not found."); return }

	srvUUID, _ := uuid.Parse(id)

	if connMode == "gsocket" && gsSecret != nil && *gsSecret != "" {
		plainSecret, decErr := app.Crypto.Decrypt(*gsSecret)
		if decErr != nil { respondError(w, 500, "DECRYPT_ERROR", "Failed to decrypt GSSocket secret."); return }
		ok, osInfo, gsUser, kernel, testErr := services.GSocketTestConnection(plainSecret, 20*time.Second)
		if !ok || testErr != nil {
			errMsg := "GSSocket connection failed."
			if testErr != nil { errMsg += " " + testErr.Error() }
			app.Pool.Exec(r.Context(), `UPDATE servers SET status='unreachable' WHERE id=$1`, id)
			respondError(w, 502, "GSOCKET_FAILED", errMsg)
			return
		}
		app.Pool.Exec(r.Context(), `UPDATE servers SET status='online', os_info=$1, kernel_info=$2, ssh_username=$3, last_checked_at=NOW() WHERE id=$4`, osInfo, kernel, gsUser, id)
		services.WriteAuditLog(r.Context(), app.Pool, "gsocket_verified", "GSSocket verified: "+name, &srvUUID, usernamePtr(r), getClientIP(r))
		respondJSON(w, 200, map[string]string{"message": "GSSocket connection successful. '" + name + "' is now monitored."})
		return
	}

	signer, _ := app.Crypto.ParsePrivateKey(encKey)
	client, err := ssh.Dial("tcp", fmt.Sprintf("%s:%d", ip, port), &ssh.ClientConfig{
		User: uname, Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: 10 * time.Second,
	})
	if err != nil { respondError(w, 502, "SSH_AUTH_FAILED", "Key auth failed. Check authorized_keys permissions (700/600)."); return }
	defer client.Close()

	osInfo, _ := services.RunOnClient(client, `cat /etc/os-release 2>/dev/null | grep PRETTY_NAME | cut -d'"' -f2 || uname -s`)
	kernel, _ := services.RunOnClient(client, `uname -r -v`)
	app.Pool.Exec(r.Context(), `UPDATE servers SET status='online', os_info=$1, kernel_info=$2, last_checked_at=NOW() WHERE id=$3`, strings.TrimSpace(osInfo), strings.TrimSpace(kernel), id)
	services.WriteAuditLog(r.Context(), app.Pool, "manual_key_verified", "Key verified: "+name, &srvUUID, usernamePtr(r), getClientIP(r))
	respondJSON(w, 200, map[string]string{"message": "SSH connection successful. '" + name + "' is now monitored."})
}

func (app *App) TestConnection(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	userID := app.getUserID(r.Context(), getUsername(r.Context()))
	var ip, uname, encKey, name, connMode string
	var port int
	var gsSecret *string
	err := app.Pool.QueryRow(r.Context(),
		`SELECT ip_address, ssh_port, ssh_username, encrypted_private_key, name, connection_mode, gsocket_secret
		 FROM servers WHERE id=$1 AND user_id=$2`, id, userID,
	).Scan(&ip, &port, &uname, &encKey, &name, &connMode, &gsSecret)
	if err != nil { respondError(w, 404, "NOT_FOUND", "Server not found."); return }

	start := time.Now()

	if connMode == "gsocket" && gsSecret != nil && *gsSecret != "" {
		plainSecret, decErr := app.Crypto.Decrypt(*gsSecret)
		if decErr != nil { respondError(w, 500, "DECRYPT_ERROR", "Failed to decrypt GSSocket secret."); return }
		ok, osInfo, gsUser, kernel, testErr := services.GSocketTestConnection(plainSecret, 20*time.Second)
		lat := float64(time.Since(start).Milliseconds())
		if !ok || testErr != nil {
			app.Pool.Exec(r.Context(), `UPDATE servers SET status='unreachable' WHERE id=$1`, id)
			errMsg := "GSSocket connection failed or timed out."
			if testErr != nil { errMsg = testErr.Error() }
			respondError(w, 502, "GSOCKET_FAILED", errMsg)
			return
		}
		app.Pool.Exec(r.Context(), `UPDATE servers SET status='online', os_info=$1, kernel_info=$2, ssh_username=$3, last_ping_ms=$4, last_checked_at=NOW(), consecutive_failures=0 WHERE id=$5`, osInfo, kernel, gsUser, lat, id)
		respondJSON(w, 200, map[string]interface{}{"status": "connected", "latency_ms": lat, "server_name": name, "mode": "gsocket"})
		return
	}

	signer, _ := app.Crypto.ParsePrivateKey(encKey)
	client, err := ssh.Dial("tcp", fmt.Sprintf("%s:%d", ip, port), &ssh.ClientConfig{
		User: uname, Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: 10 * time.Second,
	})
	if err != nil { app.Pool.Exec(r.Context(), `UPDATE servers SET status='unreachable' WHERE id=$1`, id); respondError(w, 502, "SSH_FAILED", err.Error()); return }
	defer client.Close()

	s, _ := client.NewSession(); var out bytes.Buffer; s.Stdout = &out; s.Run("echo ok"); s.Close()
	lat := float64(time.Since(start).Milliseconds())
	app.Pool.Exec(r.Context(), `UPDATE servers SET status='online', last_ping_ms=$1, last_checked_at=NOW(), consecutive_failures=0 WHERE id=$2`, lat, id)
	respondJSON(w, 200, map[string]interface{}{"status": "connected", "latency_ms": lat, "server_name": name, "mode": "ssh"})
}

func (app *App) GetPublicKey(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	userID := app.getUserID(r.Context(), getUsername(r.Context()))
	var name, pubKey string
	err := app.Pool.QueryRow(r.Context(), `SELECT name, public_key FROM servers WHERE id=$1 AND user_id=$2`, id, userID).Scan(&name, &pubKey)
	if err != nil { respondError(w, 404, "NOT_FOUND", "Server not found."); return }
	respondJSON(w, 200, map[string]string{"server_name": name, "public_key": pubKey, "install_command": fmt.Sprintf("echo '%s' >> ~/.ssh/authorized_keys", pubKey)})
}

func (app *App) verifyUserTOTP(ctx context.Context, username, code string) bool {
	var encSecret string
	if err := app.Pool.QueryRow(ctx, `SELECT totp_secret_encrypted FROM users WHERE username=$1`, username).Scan(&encSecret); err != nil { return false }
	secret, err := app.Crypto.Decrypt(encSecret)
	if err != nil { return false }
	return services.VerifyTOTP(secret, code)
}
