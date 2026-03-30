package websocket

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"vps-command/internal/config"
	"vps-command/internal/services"

	"github.com/go-chi/chi/v5"
	ws "github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/ssh"
)

var upgrader = ws.Upgrader{
	CheckOrigin:    checkWsOrigin,
	ReadBufferSize: 1024, WriteBufferSize: 1024,
}

var (
	buffers   = map[string][]map[string]interface{}{}
	buffersMu sync.RWMutex
	bufferMax = 500
)

func addToBuffer(key string, entry map[string]interface{}) {
	buffersMu.Lock()
	defer buffersMu.Unlock()
	buf := buffers[key]
	buf = append(buf, entry)
	if len(buf) > bufferMax { buf = buf[len(buf)-bufferMax:] }
	buffers[key] = buf
}

func parseLogLine(line string) map[string]interface{} {
	line = strings.TrimSpace(line)
	if line == "" { return nil }
	level := "INFO"
	upper := strings.ToUpper(line)
	switch {
	case strings.Contains(upper, "ERROR"), strings.Contains(upper, "FATAL"), strings.Contains(upper, "CRIT"):
		level = "ERROR"
	case strings.Contains(upper, "WARN"):
		level = "WARN"
	case strings.Contains(upper, "DEBUG"):
		level = "DEBUG"
	}
	timestamp := time.Now().UTC().Format(time.RFC3339)
	if m := regexp.MustCompile(`^(\w{3}\s+\d+\s+\d+:\d+:\d+)\s+`).FindStringSubmatch(line); len(m) > 1 {
		if t, err := time.Parse("2006 Jan  2 15:04:05", fmt.Sprintf("%d %s", time.Now().Year(), m[1])); err == nil {
			timestamp = t.UTC().Format(time.RFC3339)
		}
	} else if m := regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2})`).FindStringSubmatch(line); len(m) > 1 {
		timestamp = m[1]
	}
	return map[string]interface{}{
		"type": "log_line",
		"data": map[string]interface{}{"timestamp": timestamp, "level": level, "message": line},
	}
}

func HandleLogStream(cfg *config.Config, pool *pgxpool.Pool, crypto *services.CryptoService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		serverID := chi.URLParam(r, "id")
		token := r.URL.Query().Get("token")

		username, err := services.VerifyAccessToken(token, cfg.JWTSecret)
		if err != nil || username == "" {
			http.Error(w, "Invalid token", http.StatusUnauthorized)
			return
		}

		var ip, sshUser, encKey string
		var port int
		var logPathsJSON []byte
		if err := pool.QueryRow(r.Context(),
			`SELECT s.ip_address, s.ssh_port, s.ssh_username, s.encrypted_private_key, s.log_paths
			 FROM servers s JOIN users u ON s.user_id = u.id
			 WHERE s.id=$1 AND u.username=$2 AND s.status != 'pending_setup'`, serverID, username,
		).Scan(&ip, &port, &sshUser, &encKey, &logPathsJSON); err != nil {
			http.Error(w, "Server not found", http.StatusNotFound)
			return
		}

		var logPaths []string
		json.Unmarshal(logPathsJSON, &logPaths)
		if len(logPaths) == 0 { logPaths = []string{"/var/log/syslog"} }

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil { return }
		defer conn.Close()

		signer, _ := crypto.ParsePrivateKey(encKey)
		sshClient, err := ssh.Dial("tcp", fmt.Sprintf("%s:%d", ip, port), &ssh.ClientConfig{
			User: sshUser, Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)},
			HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: 10 * time.Second,
		})
		if err != nil {
			conn.WriteJSON(map[string]string{"type": "error", "message": "SSH failed: " + err.Error()})
			return
		}
		defer sshClient.Close()

		currentLogPath := logPaths[0]

		conn.WriteJSON(map[string]interface{}{
			"type": "connected", "server_id": serverID,
			"log_path": currentLogPath, "available_logs": logPaths,
		})

		paused := false
		stopCh := make(chan struct{})
		var stopOnce sync.Once
		stop := func() { stopOnce.Do(func() { close(stopCh) }) }

		session, err := sshClient.NewSession()
		if err != nil {
			conn.WriteJSON(map[string]string{"type": "error", "message": "SSH session failed."})
			return
		}
		defer session.Close()

		stdout, _ := session.StdoutPipe()
		stderr, _ := session.StderrPipe()

		// Smart log command: try journalctl first (works for non-root), then tail with permission check
		cmd := fmt.Sprintf(
			`if command -v journalctl >/dev/null 2>&1; then `+
				`journalctl -f -n 200 --no-pager 2>&1; `+
			`elif [ -r "%s" ]; then `+
				`tail -n 200 -f "%s" 2>&1; `+
			`else `+
				`echo "[SynCent] Permission denied: cannot read %s"; `+
				`echo "[SynCent] Solutions:"; `+
				`echo "[SynCent] 1. Add user to systemd-journal group: sudo usermod -aG systemd-journal %s"; `+
				`echo "[SynCent] 2. Or use a user with read access to log files"; `+
				`echo "[SynCent] 3. Or set custom log path in server settings"; `+
				`sleep infinity; `+
			`fi`,
			currentLogPath, currentLogPath, currentLogPath, sshUser)
		session.Start(cmd)

		go func() {
			defer stop()
			buf := make([]byte, 8192)
			for {
				select {
				case <-stopCh: return
				default:
				}
				n, err := stdout.Read(buf)
				if err != nil { if err != io.EOF { log.Printf("Log read: %v", err) }; return }
				if paused { continue }
				for _, line := range strings.Split(string(buf[:n]), "\n") {
					parsed := parseLogLine(line)
					if parsed == nil { continue }
					bufKey := serverID + ":" + currentLogPath
					addToBuffer(bufKey, parsed)
					if err := conn.WriteJSON(parsed); err != nil { return }
				}
			}
		}()

		go func() {
			buf := make([]byte, 4096)
			for {
				n, err := stderr.Read(buf)
				if err != nil { return }
				if n > 0 {
					msg := strings.TrimSpace(string(buf[:n]))
					if msg != "" {
						conn.WriteJSON(map[string]interface{}{
							"type": "log_line",
							"data": map[string]interface{}{
								"timestamp": time.Now().UTC().Format(time.RFC3339),
								"level": "ERROR",
								"message": "[stderr] " + msg,
							},
						})
					}
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
				case "pause":
					paused = true
					conn.WriteJSON(map[string]interface{}{"type": "status", "paused": true})
				case "resume":
					paused = false
					conn.WriteJSON(map[string]interface{}{"type": "status", "paused": false})
				case "ping":
					conn.WriteJSON(map[string]string{"type": "pong"})
				}
			}
		}()

		go func() {
			ticker := time.NewTicker(60 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-stopCh: return
				case <-ticker.C:
					if _, err := services.VerifyAccessToken(token, cfg.JWTSecret); err != nil { stop(); return }
				}
			}
		}()

		<-stopCh
	}
}
