package websocket

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"vps-command/internal/config"
	"vps-command/internal/services"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	ws "github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/ssh"
)

var (
	activeSessions   int32
	maxSessions      int32 = 3
	serverSessions   = map[string]int32{}
	serverSessionsMu sync.Mutex
	termUpgrader = ws.Upgrader{
		CheckOrigin:    checkWsOrigin,
		ReadBufferSize: 65536, WriteBufferSize: 65536,
	}
)

func checkWsOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" { return true }
	host := r.Host
	return strings.Contains(origin, host)
}

func HandleTerminal(cfg *config.Config, pool *pgxpool.Pool, crypto *services.CryptoService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		serverID := chi.URLParam(r, "id")
		token := r.URL.Query().Get("token")

		username, err := services.VerifyAccessToken(token, cfg.JWTSecret)
		if err != nil || username == "" {
			http.Error(w, "Invalid token", http.StatusUnauthorized)
			return
		}

		var count int
		pool.QueryRow(r.Context(),
			`SELECT COUNT(*) FROM servers s JOIN users u ON s.user_id = u.id WHERE s.id=$1 AND u.username=$2`,
			serverID, username).Scan(&count)
		if count == 0 {
			http.Error(w, "Server not found", http.StatusNotFound)
			return
		}

		if atomic.LoadInt32(&activeSessions) >= maxSessions {
			http.Error(w, "Max terminal sessions reached", http.StatusTooManyRequests)
			return
		}
		serverSessionsMu.Lock()
		if serverSessions[serverID] >= 1 {
			serverSessionsMu.Unlock()
			http.Error(w, "Terminal already open for this server", http.StatusConflict)
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
		if err := pool.QueryRow(r.Context(), `SELECT totp_secret_encrypted FROM users WHERE username=$1`, username).Scan(&encTOTP); err != nil {
			conn.WriteJSON(map[string]string{"type": "error", "message": "User not found."})
			return
		}
		totpSecret, _ := crypto.Decrypt(encTOTP)
		if !services.VerifyTOTP(totpSecret, authMsg.TOTPCode) {
			conn.WriteJSON(map[string]string{"type": "error", "message": "Invalid TOTP code."})
			return
		}

		var ip, sshUser, encKey, serverName string
		var port int
		if err := pool.QueryRow(r.Context(),
			`SELECT s.ip_address, s.ssh_port, s.ssh_username, s.encrypted_private_key, s.name
			 FROM servers s JOIN users u ON s.user_id = u.id
			 WHERE s.id=$1 AND u.username=$2 AND s.status != 'pending_setup'`,
			serverID, username,
		).Scan(&ip, &port, &sshUser, &encKey, &serverName); err != nil {
			conn.WriteJSON(map[string]string{"type": "error", "message": "Server not found."})
			return
		}

		signer, err := crypto.ParsePrivateKey(encKey)
		if err != nil {
			conn.WriteJSON(map[string]string{"type": "error", "message": "Key error."})
			return
		}

		sshClient, err := ssh.Dial("tcp", fmt.Sprintf("%s:%d", ip, port), &ssh.ClientConfig{
			User: sshUser, Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)},
			HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: 10 * time.Second,
		})
		if err != nil {
			conn.WriteJSON(map[string]string{"type": "error", "message": "SSH connection failed."})
			return
		}
		defer sshClient.Close()

		session, _ := sshClient.NewSession()
		defer session.Close()

		session.RequestPty("xterm-256color", 30, 120, ssh.TerminalModes{
			ssh.ECHO: 1, ssh.TTY_OP_ISPEED: 14400, ssh.TTY_OP_OSPEED: 14400,
		})

		stdinPipe, _ := session.StdinPipe()
		stdoutPipe, _ := session.StdoutPipe()

		if err := session.Shell(); err != nil {
			conn.WriteJSON(map[string]string{"type": "error", "message": "Shell failed."})
			return
		}

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

		srvUUID, _ := uuid.Parse(serverID)
		services.WriteAuditLog(context.Background(), pool, "terminal_opened",
			fmt.Sprintf("Terminal: %s@%s by %s", sshUser, serverName, username), &srvUUID, &username, "")
		sessionStart := time.Now()

		conn.WriteJSON(map[string]interface{}{"type": "connected", "server": serverName, "user": sshUser})

		stopCh := make(chan struct{})
		var stopOnce sync.Once
		stop := func() { stopOnce.Do(func() { close(stopCh) }) }
		lastInput := time.Now()

		go func() {
			defer stop()
			buf := make([]byte, 65536)
			writeErrors := 0
			for {
				n, err := stdoutPipe.Read(buf)
				if err != nil { return }
				if n > 0 {
					conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
					if err := conn.WriteMessage(ws.TextMessage, buf[:n]); err != nil {
						writeErrors++
						if writeErrors > 3 {
							conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
							conn.WriteMessage(ws.TextMessage,
								[]byte("\r\n\033[31m[SynCent] Connection lost.\033[0m\r\n"))
							return
						}
						time.Sleep(100 * time.Millisecond)
						continue
					}
					writeErrors = 0
				}
			}
		}()

		go func() {
			defer stop()
			for {
				msgType, msg, err := conn.ReadMessage()
				if err != nil { return }
				if msgType == ws.TextMessage {
					var ctrl struct { Type string `json:"type"`; Cols int `json:"cols"`; Rows int `json:"rows"` }
					if json.Unmarshal(msg, &ctrl) == nil && ctrl.Type != "" {
						if ctrl.Type == "resize" && ctrl.Cols > 0 && ctrl.Rows > 0 { session.WindowChange(ctrl.Rows, ctrl.Cols) }
						continue
					}
				}
				lastInput = time.Now()
				stdinPipe.Write(msg)
			}
		}()

		go func() {
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-stopCh: return
				case <-ticker.C:
					if _, err := services.VerifyAccessToken(token, cfg.JWTSecret); err != nil { stop(); return }
					if time.Since(lastInput) > 15*time.Minute { stop(); return }
				}
			}
		}()

		go func() { session.Wait(); stop() }()
		<-stopCh

		log.Printf("Terminal closed: %s (%s)", serverName, time.Since(sessionStart).Round(time.Second))
		services.WriteAuditLog(context.Background(), pool, "terminal_closed",
			fmt.Sprintf("Terminal closed: %s (%s)", serverName, time.Since(sessionStart).Round(time.Second)), &srvUUID, &username, "")
	}
}
