package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"vps-command/internal/services"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// ── Metrics ──
func (app *App) verifyServerAccess(r *http.Request) (string, string, error) {
	id := chi.URLParam(r, "id")
	userID := app.getUserID(r.Context(), getUsername(r.Context()))
	var name string
	err := app.Pool.QueryRow(r.Context(), `SELECT name FROM servers WHERE id=$1 AND user_id=$2`, id, userID).Scan(&name)
	return id, name, err
}

func (app *App) GetCurrentMetrics(w http.ResponseWriter, r *http.Request) {
	id, serverName, err := app.verifyServerAccess(r)
	if err != nil { respondError(w, 404, "NOT_FOUND", "Server not found."); return }

	var status string; var pingMs *float64
	app.Pool.QueryRow(r.Context(), `SELECT status, last_ping_ms FROM servers WHERE id=$1`, id).Scan(&status, &pingMs)

	row := app.Pool.QueryRow(r.Context(),
		`SELECT cpu_percent, ram_percent, ram_used_mb, ram_total_mb, disk_percent, disk_used_gb, disk_total_gb,
		 net_in_bytes, net_out_bytes, disk_io_read_bytes, disk_io_write_bytes, top_processes, recorded_at
		 FROM metrics WHERE server_id=$1 ORDER BY recorded_at DESC LIMIT 1`, id)

	var cpu, ramPct, diskPct, diskUsed, diskTotal *float64
	var ramUsed, ramTotal *int
	var netIn, netOut, ioRead, ioWrite *int64
	var procs json.RawMessage
	var recordedAt *time.Time

	err = row.Scan(&cpu, &ramPct, &ramUsed, &ramTotal, &diskPct, &diskUsed, &diskTotal,
		&netIn, &netOut, &ioRead, &ioWrite, &procs, &recordedAt)
	if err != nil {
		respondJSON(w, 200, map[string]interface{}{
			"server_name": serverName, "status": status, "metrics": nil,
			"message": "No metrics yet. Collected every 5 minutes.",
		})
		return
	}

	respondJSON(w, 200, map[string]interface{}{
		"server_name": serverName, "status": status, "last_ping_ms": pingMs,
		"metrics": map[string]interface{}{
			"cpu_percent": cpu, "ram_percent": ramPct, "ram_used_mb": ramUsed, "ram_total_mb": ramTotal,
			"disk_percent": diskPct, "disk_used_gb": diskUsed, "disk_total_gb": diskTotal,
			"net_in_bytes": netIn, "net_out_bytes": netOut,
			"disk_io_read_bytes": ioRead, "disk_io_write_bytes": ioWrite,
			"top_processes": procs, "recorded_at": recordedAt,
		},
	})
}

func (app *App) GetMetricsHistory(w http.ResponseWriter, r *http.Request) {
	id, _, err := app.verifyServerAccess(r)
	if err != nil { respondError(w, 404, "NOT_FOUND", "Server not found."); return }
	rng := r.URL.Query().Get("range")
	if rng == "" { rng = "24h" }

	durations := map[string]time.Duration{
		"1h": time.Hour, "6h": 6 * time.Hour, "24h": 24 * time.Hour,
		"7d": 7 * 24 * time.Hour, "30d": 30 * 24 * time.Hour,
	}
	dur, ok := durations[rng]
	if !ok { dur = 24 * time.Hour }

	since := time.Now().Add(-dur)
	rows, _ := app.Pool.Query(r.Context(),
		`SELECT cpu_percent, ram_percent, disk_percent, net_in_bytes, net_out_bytes, recorded_at
		 FROM metrics WHERE server_id=$1 AND recorded_at >= $2 ORDER BY recorded_at ASC`, id, since)
	defer rows.Close()

	var data []map[string]interface{}
	for rows.Next() {
		var cpu, ram, disk *float64
		var netIn, netOut *int64
		var ts time.Time
		rows.Scan(&cpu, &ram, &disk, &netIn, &netOut, &ts)
		data = append(data, map[string]interface{}{
			"timestamp": ts, "cpu": cpu, "ram": ram, "disk": disk, "net_in": netIn, "net_out": netOut,
		})
	}
	if data == nil { data = []map[string]interface{}{} }
	respondJSON(w, 200, map[string]interface{}{"range": rng, "count": len(data), "data": data})
}

func (app *App) GetAvailability(w http.ResponseWriter, r *http.Request) {
	id, _, err := app.verifyServerAccess(r)
	if err != nil { respondError(w, 404, "NOT_FOUND", "Server not found."); return }

	var name, status string
	app.Pool.QueryRow(r.Context(), `SELECT name, status FROM servers WHERE id=$1`, id).Scan(&name, &status)

	avail := map[string]float64{}
	for _, label := range []string{"24h", "7d", "30d"} {
		dur := map[string]time.Duration{"24h": 24 * time.Hour, "7d": 7 * 24 * time.Hour, "30d": 30 * 24 * time.Hour}[label]
		since := time.Now().Add(-dur)
		avail[label] = app.calcAvailability(r.Context(), id, since, status)
	}
	respondJSON(w, 200, map[string]interface{}{"server_name": name, "status": status, "availability": avail})
}

func (app *App) calcAvailability(ctx context.Context, serverID string, since time.Time, currentStatus string) float64 {
	rows, err := app.Pool.Query(ctx,
		`SELECT status, changed_at FROM status_history WHERE server_id=$1 AND changed_at >= $2 ORDER BY changed_at ASC`,
		serverID, since)
	if err != nil { return 0 }
	defer rows.Close()

	type transition struct { Status string; At time.Time }
	var transitions []transition
	for rows.Next() {
		var t transition
		rows.Scan(&t.Status, &t.At)
		transitions = append(transitions, t)
	}

	now := time.Now()
	total := now.Sub(since).Seconds()
	if len(transitions) == 0 {
		if currentStatus == "online" { return 100 }
		return 0
	}

	prevStatus := "unknown"
	var prev string
	err = app.Pool.QueryRow(ctx,
		`SELECT status FROM status_history WHERE server_id=$1 AND changed_at < $2 ORDER BY changed_at DESC LIMIT 1`,
		serverID, since).Scan(&prev)
	if err == nil { prevStatus = prev }

	var onlineSec float64
	prevTime := since
	for _, t := range transitions {
		if prevStatus == "online" {
			onlineSec += t.At.Sub(prevTime).Seconds()
		}
		prevTime = t.At
		prevStatus = t.Status
	}
	if prevStatus == "online" {
		onlineSec += now.Sub(prevTime).Seconds()
	}
	if total > 0 {
		return float64(int(onlineSec/total*10000)) / 100
	}
	return 0
}

func (app *App) CollectMetricsNow(w http.ResponseWriter, r *http.Request) {
	id, _, err := app.verifyServerAccess(r)
	if err != nil { respondError(w, 404, "NOT_FOUND", "Server not found."); return }
	uid, _ := uuid.Parse(id)

	if err := app.Health.CollectMetricsForServer(r.Context(), uid); err != nil {
		respondError(w, 400, "COLLECT_ERROR", "Metric collection failed.")
		return
	}
	app.GetCurrentMetrics(w, r)
}

func (app *App) ExecuteCommand(w http.ResponseWriter, r *http.Request) {
	id, _, accessErr := app.verifyServerAccess(r)
	if accessErr != nil { respondError(w, 404, "NOT_FOUND", "Server not found."); return }

	var body struct {
		Command      string `json:"command"`
		Timeout      int    `json:"timeout"`
		TOTPCode     string `json:"totp_code"`
		Confirmation string `json:"confirmation"`
	}
	if err := decodeJSON(r, &body); err != nil || body.Command == "" {
		respondError(w, 400, "INVALID_INPUT", "Command required.")
		return
	}
	if body.Timeout == 0 { body.Timeout = 30 }

	tier := services.ClassifyCommand(body.Command)

	if tier == services.TierBlocked {
		respondError(w, 403, "COMMAND_BLOCKED",
			fmt.Sprintf("Blocked: '%s'", truncate(body.Command, 50)),
			"Too destructive for dashboard. SSH in directly if needed.")
		return
	}

	if tier == services.TierDangerous {
		username := getUsername(r.Context())
		if !app.VerifyAction(r.Context(), username, body.TOTPCode, body.Confirmation, "EXECUTE") {
			if app.is2FARequired() {
				respondError(w, 403, "TOTP_REQUIRED", "This command requires TOTP verification.")
			} else {
				respondError(w, 403, "CONFIRM_REQUIRED", "Type 'EXECUTE' to confirm this dangerous command.")
			}
			return
		}
	}

	userID := app.getUserID(r.Context(), getUsername(r.Context()))
	var ip, sshUsername, encKey, connMode string
	var port int
	var gsSecret *string
	err := app.Pool.QueryRow(r.Context(),
		`SELECT ip_address, ssh_port, ssh_username, encrypted_private_key, connection_mode, gsocket_secret
		 FROM servers WHERE id=$1 AND user_id=$2`, id, userID,
	).Scan(&ip, &port, &sshUsername, &encKey, &connMode, &gsSecret)
	if err != nil {
		respondError(w, 404, "NOT_FOUND", "Server not found.")
		return
	}

	start := time.Now()

	if connMode == "gsocket" && gsSecret != nil && *gsSecret != "" {
		plainSecret, decErr := app.Crypto.Decrypt(*gsSecret)
		if decErr != nil { respondError(w, 500, "DECRYPT_ERROR", "Failed to decrypt GSSocket secret."); return }
		stdout, stderr, exitCode, gsErr := services.GSocketExecute(
			r.Context(), plainSecret, body.Command, time.Duration(body.Timeout)*time.Second)
		if gsErr != nil {
			respondError(w, 502, "GSOCKET_ERROR", "Execution failed: "+gsErr.Error())
			return
		}
		durationMs := time.Since(start).Milliseconds()
		serverUUID, _ := uuid.Parse(id)
		services.WriteAuditLog(r.Context(), app.Pool, "command_executed",
			fmt.Sprintf("[%s/gsocket] %s → exit %d (%dms)", tier, truncate(body.Command, 200), exitCode, durationMs),
			&serverUUID, usernamePtr(r), getClientIP(r))
		respondJSON(w, 200, map[string]interface{}{
			"stdout": stdout, "stderr": stderr,
			"exit_code": exitCode, "duration_ms": durationMs,
			"command_tier": string(tier),
		})
		return
	}

	result, err := app.SSH.ExecuteCommand(r.Context(), ip, port, sshUsername, encKey, body.Command, id, time.Duration(body.Timeout)*time.Second)
	if err != nil {
		respondError(w, 502, "SSH_ERROR", "Command execution failed. Check server connectivity.")
		return
	}

	serverUUID, _ := uuid.Parse(id)
	services.WriteAuditLog(r.Context(), app.Pool, "command_executed",
		fmt.Sprintf("[%s] %s → exit %d (%dms)", tier, truncate(body.Command, 200), result.ExitCode, result.DurationMs),
		&serverUUID, usernamePtr(r), getClientIP(r))

	respondJSON(w, 200, map[string]interface{}{
		"stdout": result.Stdout, "stderr": result.Stderr,
		"exit_code": result.ExitCode, "duration_ms": result.DurationMs,
		"command_tier": string(tier),
	})
}

func (app *App) ClassifyCommandPreview(w http.ResponseWriter, r *http.Request) {
	cmd := r.URL.Query().Get("command")
	tier := services.ClassifyCommand(cmd)
	respondJSON(w, 200, map[string]interface{}{
		"command": cmd, "tier": string(tier),
		"requires_totp": tier == services.TierDangerous,
		"blocked":       tier == services.TierBlocked,
	})
}

func (app *App) ListSnippets(w http.ResponseWriter, r *http.Request) {
	userID := app.getUserID(r.Context(), getUsername(r.Context()))
	rows, _ := app.Pool.Query(r.Context(), `SELECT id, name, command, category, is_dangerous, created_at FROM snippets WHERE user_id=$1 OR user_id IS NULL ORDER BY category, name`, userID)
	defer rows.Close()
	var snippets []map[string]interface{}
	for rows.Next() {
		var id uuid.UUID; var name, command, category string; var dangerous bool; var created time.Time
		rows.Scan(&id, &name, &command, &category, &dangerous, &created)
		snippets = append(snippets, map[string]interface{}{
			"id": id, "name": name, "command": command, "category": category,
			"is_dangerous": dangerous, "created_at": created,
		})
	}
	if snippets == nil { snippets = []map[string]interface{}{} }
	respondJSON(w, 200, snippets)
}

func (app *App) CreateSnippet(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name     string `json:"name"`
		Command  string `json:"command"`
		Category string `json:"category"`
	}
	if err := decodeJSON(r, &body); err != nil || body.Name == "" || body.Command == "" {
		respondError(w, 400, "INVALID_INPUT", "Name and command required.")
		return
	}
	if body.Category == "" { body.Category = "custom" }

	tier := services.ClassifyCommand(body.Command)
	if tier == services.TierBlocked {
		respondError(w, 403, "COMMAND_BLOCKED", "Cannot save blocked command.")
		return
	}

	var id uuid.UUID
	userID := app.getUserID(r.Context(), getUsername(r.Context()))
	app.Pool.QueryRow(r.Context(),
		`INSERT INTO snippets (name, command, category, is_dangerous, user_id) VALUES ($1,$2,$3,$4,$5) RETURNING id`,
		body.Name, body.Command, body.Category, tier == services.TierDangerous, userID).Scan(&id)

	respondJSON(w, 201, map[string]interface{}{
		"id": id, "name": body.Name, "tier": string(tier), "message": "Snippet saved.",
	})
}

func (app *App) DeleteSnippet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "snippetId")
	userID := app.getUserID(r.Context(), getUsername(r.Context()))
	app.Pool.Exec(r.Context(), `DELETE FROM snippets WHERE id=$1 AND (user_id=$2 OR user_id IS NULL)`, id, userID)
	respondJSON(w, 200, map[string]string{"message": "Snippet deleted."})
}

func truncate(s string, n int) string {
	if len(s) <= n { return s }
	return s[:n] + "..."
}
