package handlers

import (
	"net/http"
	"strconv"
	"time"

	"vps-command/internal/services"

	"github.com/google/uuid"
)

func (app *App) GetAuditLog(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if page < 1 { page = 1 }
	if limit < 1 || limit > 100 { limit = 50 }
	offset := (page - 1) * limit
	userID := app.getUserID(r.Context(), getUsername(r.Context()))

	rows, err := app.Pool.Query(r.Context(),
		`SELECT a.id, a.server_id, a.action, COALESCE(a.detail,''), a.source_ip, a.created_at
		 FROM audit_log a
		 WHERE a.user_id=$1 OR a.user_id IS NULL
		 ORDER BY a.created_at DESC LIMIT $2 OFFSET $3`, userID, limit, offset)
	if err != nil { respondError(w, 500, "DB_ERROR", "Failed to fetch audit log."); return }
	defer rows.Close()

	var entries []map[string]interface{}
	for rows.Next() {
		var id int64; var serverID *uuid.UUID; var action, detail, sourceIP string; var createdAt time.Time
		rows.Scan(&id, &serverID, &action, &detail, &sourceIP, &createdAt)
		entries = append(entries, map[string]interface{}{"id": id, "server_id": serverID, "action": action, "detail": detail, "source_ip": sourceIP, "created_at": createdAt})
	}
	if entries == nil { entries = []map[string]interface{}{} }
	var total int
	app.Pool.QueryRow(r.Context(), `SELECT COUNT(*) FROM audit_log WHERE user_id=$1 OR user_id IS NULL`, userID).Scan(&total)
	respondJSON(w, 200, map[string]interface{}{"entries": entries, "page": page, "limit": limit, "total": total})
}

func (app *App) GetServerHistory(w http.ResponseWriter, r *http.Request) {
	id, _, err := app.verifyServerAccess(r)
	if err != nil { respondError(w, 404, "NOT_FOUND", "Server not found."); return }

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if page < 1 { page = 1 }
	if limit < 1 || limit > 100 { limit = 50 }

	rows, err := app.Pool.Query(r.Context(),
		`SELECT id, action, COALESCE(detail,''), source_ip, created_at
		 FROM audit_log WHERE server_id=$1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
		id, limit, (page-1)*limit)
	if err != nil { respondError(w, 500, "DB_ERROR", "Failed."); return }
	defer rows.Close()

	var entries []map[string]interface{}
	for rows.Next() {
		var eid int64; var action, detail, ip string; var at time.Time
		rows.Scan(&eid, &action, &detail, &ip, &at)
		entries = append(entries, map[string]interface{}{"id": eid, "action": action, "detail": detail, "source_ip": ip, "created_at": at})
	}
	if entries == nil { entries = []map[string]interface{}{} }
	respondJSON(w, 200, entries)
}

func (app *App) GetStatusHistory(w http.ResponseWriter, r *http.Request) {
	id, _, err := app.verifyServerAccess(r)
	if err != nil { respondError(w, 404, "NOT_FOUND", "Server not found."); return }

	rng := r.URL.Query().Get("range")
	if rng == "" { rng = "7d" }
	durs := map[string]time.Duration{"24h": 24 * time.Hour, "7d": 7 * 24 * time.Hour, "30d": 30 * 24 * time.Hour}
	dur := durs[rng]; if dur == 0 { dur = 7 * 24 * time.Hour }

	rows, _ := app.Pool.Query(r.Context(),
		`SELECT status, changed_at FROM status_history WHERE server_id=$1 AND changed_at >= $2 ORDER BY changed_at ASC`,
		id, time.Now().Add(-dur))
	defer rows.Close()

	var transitions []map[string]interface{}
	for rows.Next() {
		var status string; var at time.Time
		rows.Scan(&status, &at)
		transitions = append(transitions, map[string]interface{}{"status": status, "changed_at": at})
	}
	if transitions == nil { transitions = []map[string]interface{}{} }
	respondJSON(w, 200, map[string]interface{}{"range": rng, "transitions": transitions})
}

func (app *App) VerifyAuditChain(w http.ResponseWriter, r *http.Request) {
	result, err := services.VerifyAuditChain(r.Context(), app.Pool)
	if err != nil { respondError(w, 500, "VERIFY_ERROR", "Audit verification failed."); return }
	respondJSON(w, 200, result)
}

func (app *App) DashboardSummary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID := app.getUserID(ctx, getUsername(ctx))

	var total, online, offline, unreachable, pending int
	app.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM servers WHERE user_id=$1`, userID).Scan(&total)
	app.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM servers WHERE user_id=$1 AND status='online'`, userID).Scan(&online)
	app.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM servers WHERE user_id=$1 AND status IN ('offline','unreachable','ssh_down')`, userID).Scan(&offline)
	app.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM servers WHERE user_id=$1 AND status='unreachable'`, userID).Scan(&unreachable)
	app.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM servers WHERE user_id=$1 AND status='pending_setup'`, userID).Scan(&pending)

	rows, _ := app.Pool.Query(ctx,
		`SELECT action, COALESCE(detail,''), created_at FROM audit_log
		 WHERE user_id=$1 OR user_id IS NULL ORDER BY created_at DESC LIMIT 10`, userID)
	defer rows.Close()
	var recent []map[string]interface{}
	for rows.Next() {
		var action, detail string; var at time.Time
		rows.Scan(&action, &detail, &at)
		recent = append(recent, map[string]interface{}{"action": action, "detail": detail, "created_at": at})
	}
	if recent == nil { recent = []map[string]interface{}{} }

	staleT := time.Now().Add(-time.Duration(app.Config.StaleServerDays) * 24 * time.Hour)
	var stale int
	app.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM servers WHERE user_id=$1 AND status IN ('offline','unreachable','ssh_down') AND last_checked_at IS NOT NULL AND last_checked_at < $2`,
		userID, staleT).Scan(&stale)

	respondJSON(w, 200, map[string]interface{}{
		"servers": map[string]int{"total": total, "online": online, "offline": offline, "unreachable": unreachable, "pending_setup": pending, "stale": stale},
		"recent_activity": recent,
	})
}
