package websocket

import (
	"encoding/json"
	"log"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"vps-command/internal/config"
	"vps-command/internal/services"

	"github.com/go-chi/chi/v5"
	ws "github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
)

func isGsNoise(line string) bool {
	if line == "" { return true }
	checks := []string{"=Secret", "=Encryption", "=Hint", "GSRN connection",
		"Connecting to GSRN", "Disconnected after", "takes longer", "PS1=", "[?2004"}
	for _, c := range checks {
		if strings.Contains(line, c) { return true }
	}
	return false
}

func HandleGSTerminal(cfg *config.Config, pool *pgxpool.Pool, crypto *services.CryptoService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		serverID := chi.URLParam(r, "id")
		token := r.URL.Query().Get("token")

		username, err := services.VerifyAccessToken(token, cfg.JWTSecret)
		if err != nil || username == "" {
			http.Error(w, "Invalid token", http.StatusUnauthorized)
			return
		}

		var encGsSecret, serverName string
		err = pool.QueryRow(r.Context(),
			`SELECT s.gsocket_secret, s.name FROM servers s JOIN users u ON s.user_id = u.id
			 WHERE s.id=$1 AND u.username=$2 AND s.connection_mode='gsocket' AND s.status != 'pending_setup'`,
			serverID, username).Scan(&encGsSecret, &serverName)
		if err != nil || encGsSecret == "" {
			http.Error(w, "GSSocket server not found", http.StatusNotFound)
			return
		}

		if atomic.LoadInt32(&activeSessions) >= maxSessions {
			http.Error(w, "Max terminal sessions reached", http.StatusTooManyRequests)
			return
		}
		serverSessionsMu.Lock()
		if serverSessions[serverID] >= 1 {
			serverSessionsMu.Unlock()
			http.Error(w, "Terminal already open", http.StatusConflict)
			return
		}
		serverSessionsMu.Unlock()

		conn, err := termUpgrader.Upgrade(w, r, nil)
		if err != nil { return }
		defer conn.Close()

		conn.WriteJSON(map[string]string{"type": "auth_required", "message": "Send TOTP code."})
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		_, msg, err := conn.ReadMessage()
		if err != nil { return }
		conn.SetReadDeadline(time.Time{})

		var authMsg struct { Type string `json:"type"`; TOTPCode string `json:"totp_code"` }
		if err := json.Unmarshal(msg, &authMsg); err != nil || authMsg.TOTPCode == "" {
			conn.WriteJSON(map[string]string{"type": "error", "message": "Invalid format."})
			return
		}

		var encTOTP string
		pool.QueryRow(r.Context(), `SELECT totp_secret_encrypted FROM users WHERE username=$1`, username).Scan(&encTOTP)
		totpSecret, _ := crypto.Decrypt(encTOTP)
		if !services.VerifyTOTP(totpSecret, authMsg.TOTPCode) {
			conn.WriteJSON(map[string]string{"type": "error", "message": "Invalid TOTP code."})
			return
		}

		gsSecret, decErr := crypto.Decrypt(encGsSecret)
		if decErr != nil {
			conn.WriteJSON(map[string]string{"type": "error", "message": "Decryption error."})
			return
		}

		conn.WriteJSON(map[string]interface{}{"type": "connected", "server": serverName, "mode": "gsocket"})

		atomic.AddInt32(&activeSessions, 1)
		serverSessionsMu.Lock()
		serverSessions[serverID]++
		serverSessionsMu.Unlock()
		defer func() {
			atomic.AddInt32(&activeSessions, -1)
			serverSessionsMu.Lock()
			serverSessions[serverID]--
			if serverSessions[serverID] <= 0 { delete(serverSessions, serverID) }
			serverSessionsMu.Unlock()
		}()

		cmd := exec.Command("gs-netcat", "-s", gsSecret, "-i")
		stdin, _ := cmd.StdinPipe()
		stdout, _ := cmd.StdoutPipe()
		stderr, _ := cmd.StderrPipe()
		if err := cmd.Start(); err != nil {
			conn.WriteJSON(map[string]string{"type": "error", "message": "gs-netcat failed."})
			return
		}

		stopCh := make(chan struct{})
		var stopOnce sync.Once
		stop := func() { stopOnce.Do(func() { close(stopCh) }) }

		go func() {
			defer stop()
			buf := make([]byte, 65536)
			for {
				n, err := stdout.Read(buf)
				if err != nil { return }
				if n > 0 {
					conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
					if err := conn.WriteMessage(ws.TextMessage, buf[:n]); err != nil { return }
				}
			}
		}()

		go func() {
			buf := make([]byte, 4096)
			for { n, err := stderr.Read(buf); if err != nil { return }; if n > 0 { conn.WriteMessage(ws.TextMessage, buf[:n]) } }
		}()

		go func() {
			defer stop()
			for {
				_, msg, err := conn.ReadMessage()
				if err != nil { return }
				stdin.Write(msg)
			}
		}()

		go func() { cmd.Wait(); stop() }()
		go func() {
			ticker := time.NewTicker(60 * time.Second)
			defer ticker.Stop()
			for { select { case <-stopCh: return; case <-ticker.C: if _, err := services.VerifyAccessToken(token, cfg.JWTSecret); err != nil { stop(); return } } }
		}()

		<-stopCh
		stdin.Close()
		cmd.Process.Kill()
		log.Printf("GS Terminal closed: %s", serverName)
	}
}

func HandleGSLogs(cfg *config.Config, pool *pgxpool.Pool, crypto *services.CryptoService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		serverID := chi.URLParam(r, "id")
		token := r.URL.Query().Get("token")

		username, err := services.VerifyAccessToken(token, cfg.JWTSecret)
		if err != nil || username == "" {
			http.Error(w, "Invalid token", http.StatusUnauthorized)
			return
		}

		var encGsSecret, serverName string
		pool.QueryRow(r.Context(),
			`SELECT s.gsocket_secret, s.name FROM servers s JOIN users u ON s.user_id = u.id
			 WHERE s.id=$1 AND u.username=$2 AND s.connection_mode='gsocket' AND s.status != 'pending_setup'`,
			serverID, username).Scan(&encGsSecret, &serverName)
		if encGsSecret == "" {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}

		gsSecret, decErr := crypto.Decrypt(encGsSecret)
		if decErr != nil {
			http.Error(w, "Decryption error", http.StatusInternalServerError)
			return
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil { return }
		defer conn.Close()

		conn.WriteJSON(map[string]interface{}{
			"type": "connected", "server_id": serverID,
			"log_path": "journalctl", "available_logs": []string{"journalctl"},
		})

		logScript := "export TERM=dumb\nunset PS1\nstty -echo 2>/dev/null\njournalctl -f -n 200 --no-pager 2>&1 || tail -f /var/log/syslog 2>&1 || echo '[SynCent] Cannot access logs'\n"
		cmd := exec.Command("gs-netcat", "-s", gsSecret, "-i")
		cmd.Stdin = strings.NewReader(logScript)
		stdout, _ := cmd.StdoutPipe()
		cmd.Start()

		stopCh := make(chan struct{})
		var stopOnce sync.Once
		stop := func() { stopOnce.Do(func() { close(stopCh) }) }
		paused := false
		headerDone := false
		lineCount := 0

		go func() {
			defer stop()
			buf := make([]byte, 8192)
			for {
				n, err := stdout.Read(buf)
				if err != nil { return }
				if paused { continue }
				text := services.StripANSI(string(buf[:n]))
				for _, line := range strings.Split(text, "\n") {
					line = strings.TrimSpace(line)
					if isGsNoise(line) { continue }
					if strings.HasPrefix(line, "export TERM") { continue }
					if strings.HasPrefix(line, "unset PS1") { continue }
					if strings.HasPrefix(line, "stty ") { continue }
					if strings.HasPrefix(line, "journalctl") { continue }
					if strings.HasPrefix(line, "tail -f") { continue }
					if !headerDone {
						lineCount++
						if lineCount > 5 { headerDone = true }
						if strings.Contains(line, "@") && (strings.Contains(line, "$") || strings.Contains(line, "#")) { continue }
					}
					parsed := parseLogLine(line)
					if parsed == nil { continue }
					if err := conn.WriteJSON(parsed); err != nil { return }
				}
			}
		}()

		go func() {
			defer stop()
			for {
				_, msg, err := conn.ReadMessage()
				if err != nil { return }
				var data map[string]string
				if json.Unmarshal(msg, &data) != nil { continue }
				switch data["type"] {
				case "pause": paused = true
				case "resume": paused = false
				case "ping": conn.WriteJSON(map[string]string{"type": "pong"})
				}
			}
		}()

		go func() { cmd.Wait(); stop() }()
		<-stopCh
		cmd.Process.Kill()
	}
}
