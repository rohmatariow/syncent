package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ParsedMetrics struct {
	CPU        *float64          `json:"cpu_percent"`
	RAMPercent *float64          `json:"ram_percent"`
	RAMUsedMB  *int              `json:"ram_used_mb"`
	RAMTotalMB *int              `json:"ram_total_mb"`
	DiskPct    *float64          `json:"disk_percent"`
	DiskUsedGB *float64          `json:"disk_used_gb"`
	DiskTotGB  *float64          `json:"disk_total_gb"`
	NetIn      *int64            `json:"net_in_bytes"`
	NetOut     *int64            `json:"net_out_bytes"`
	DiskIORead *int64            `json:"disk_io_read_bytes"`
	DiskIOWr   *int64            `json:"disk_io_write_bytes"`
	TopProcs   []json.RawMessage `json:"top_processes"`
}

func ParseCPU(output string) *float64 {
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "%Cpu") || strings.Contains(line, "Cpu(s)") {
			re := regexp.MustCompile(`(\d+\.?\d*)\s*(id|idle)`)
			if m := re.FindStringSubmatch(line); len(m) > 1 {
				if idle, err := strconv.ParseFloat(m[1], 64); err == nil {
					v := 100.0 - idle
					return &v
				}
			}
		}
	}
	return nil
}

func ParseMemory(output string) (*float64, *int, *int) {
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "Mem:") {
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				total, e1 := strconv.ParseInt(parts[1], 10, 64)
				used, e2 := strconv.ParseInt(parts[2], 10, 64)
				if e1 == nil && e2 == nil && total > 0 {
					pct := float64(used) / float64(total) * 100
					usedMB := int(used / (1024 * 1024))
					totalMB := int(total / (1024 * 1024))
					return &pct, &usedMB, &totalMB
				}
			}
		}
	}
	return nil, nil, nil
}

func ParseDisk(output string) (*float64, *float64, *float64) {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) < 2 {
		return nil, nil, nil
	}
	// Parse last line (data line)
	parts := strings.Fields(lines[len(lines)-1])
	for i := 0; i < len(parts)-2; i++ {
		total, e1 := strconv.ParseInt(parts[i], 10, 64)
		used, e2 := strconv.ParseInt(parts[i+1], 10, 64)
		if e1 == nil && e2 == nil && total > 0 {
			pct := float64(used) / float64(total) * 100
			usedGB := float64(used) / (1024 * 1024 * 1024)
			totalGB := float64(total) / (1024 * 1024 * 1024)
			return &pct, &usedGB, &totalGB
		}
	}
	return nil, nil, nil
}

func ParseNetwork(output string) (*int64, *int64) {
	var totalIn, totalOut int64
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, ":") && !strings.Contains(line, "lo:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				fields := strings.Fields(parts[1])
				if len(fields) >= 9 {
					if in, err := strconv.ParseInt(fields[0], 10, 64); err == nil {
						totalIn += in
					}
					if out, err := strconv.ParseInt(fields[8], 10, 64); err == nil {
						totalOut += out
					}
				}
			}
		}
	}
	if totalIn > 0 || totalOut > 0 {
		return &totalIn, &totalOut
	}
	return nil, nil
}

func ParseDiskIO(output string) (*int64, *int64) {
	var totalRead, totalWrite int64
	re := regexp.MustCompile(`^(sd[a-z]|vd[a-z]|nvme\d+n\d+|xvd[a-z])$`)
	for _, line := range strings.Split(output, "\n") {
		parts := strings.Fields(line)
		if len(parts) >= 14 {
			if re.MatchString(parts[2]) {
				if r, err := strconv.ParseInt(parts[5], 10, 64); err == nil {
					totalRead += r * 512
				}
				if w, err := strconv.ParseInt(parts[9], 10, 64); err == nil {
					totalWrite += w * 512
				}
			}
		}
	}
	if totalRead > 0 || totalWrite > 0 {
		return &totalRead, &totalWrite
	}
	return nil, nil
}

func ParseTopProcesses(output string) []json.RawMessage {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	var procs []json.RawMessage
	for _, line := range lines[1:] {
		parts := strings.Fields(line)
		if len(parts) >= 11 {
			cpu, _ := strconv.ParseFloat(parts[2], 64)
			ram, _ := strconv.ParseFloat(parts[3], 64)
			name := strings.Join(parts[10:], " ")
			if len(name) > 50 {
				name = name[:50]
			}
			p, _ := json.Marshal(map[string]interface{}{
				"name": name, "cpu": cpu, "ram": ram,
			})
			procs = append(procs, p)
		}
		if len(procs) >= 5 {
			break
		}
	}
	return procs
}

type HealthChecker struct {
	ssh       *SSHManager
	crypto    *CryptoService
	pool      *pgxpool.Pool
	threshold int
	stopCh    chan struct{}
}

func NewHealthChecker(ssh *SSHManager, crypto *CryptoService, pool *pgxpool.Pool, threshold int) *HealthChecker {
	return &HealthChecker{
		ssh:       ssh,
		crypto:    crypto,
		pool:      pool,
		threshold: threshold,
		stopCh:    make(chan struct{}),
	}
}

func (h *HealthChecker) Start() {
	go h.pingLoop()
	go h.tcpLoop()
	go h.metricsLoop()
	log.Println("Health checker started.")
}

func (h *HealthChecker) Stop() {
	close(h.stopCh)
	log.Println("Health checker stopped.")
}

func (h *HealthChecker) pingLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-h.stopCh:
			return
		case <-ticker.C:
			h.runPings()
		}
	}
}

func (h *HealthChecker) tcpLoop() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-h.stopCh:
			return
		case <-ticker.C:
			h.runTCPChecks()
		}
	}
}

func (h *HealthChecker) metricsLoop() {
	select {
	case <-h.stopCh:
		return
	case <-time.After(10 * time.Second):
	}

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	h.runMetricsCollection()
	for {
		select {
		case <-h.stopCh:
			return
		case <-ticker.C:
			h.runMetricsCollection()
		}
	}
}

type serverRow struct {
	ID            uuid.UUID
	Name          string
	IP            string
	Port          int
	Username      string
	EncKey        *string
	Status        string
	ConsecFails   int
	ExpServices   []byte
	ConnMode      string
	GSocketSecret *string
}

func (h *HealthChecker) getActiveServers() ([]serverRow, error) {
	rows, err := h.pool.Query(context.Background(),
		`SELECT id, name, ip_address, ssh_port, ssh_username, encrypted_private_key, status, consecutive_failures, expected_services, connection_mode, gsocket_secret
		 FROM servers WHERE status != 'pending_setup'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var servers []serverRow
	for rows.Next() {
		var s serverRow
		if err := rows.Scan(&s.ID, &s.Name, &s.IP, &s.Port, &s.Username, &s.EncKey, &s.Status, &s.ConsecFails, &s.ExpServices, &s.ConnMode, &s.GSocketSecret); err != nil {
			continue
		}
		servers = append(servers, s)
	}
	return servers, nil
}

func (h *HealthChecker) runPings() {
	servers, err := h.getActiveServers()
	if err != nil {
		log.Printf("Ping: fetch servers error: %v", err)
		return
	}
	for _, s := range servers {
		go func(srv serverRow) {
			if srv.ConnMode == "gsocket" { return }
			reachable, latency := h.ssh.PingICMP(srv.IP)
			ctx := context.Background()
			h.pool.Exec(ctx,
				`UPDATE servers SET last_ping_ms=$1, last_checked_at=NOW() WHERE id=$2`,
				latency, srv.ID)
			if !reachable {
				h.handleFailure(ctx, srv, "unreachable")
			}
		}(s)
	}
}

func (h *HealthChecker) runTCPChecks() {
	servers, err := h.getActiveServers()
	if err != nil {
		return
	}
	for _, s := range servers {
		go func(srv serverRow) {
			if srv.ConnMode == "gsocket" { return }
			open, latency := h.ssh.PingTCP(srv.IP, srv.Port)
			ctx := context.Background()
			h.pool.Exec(ctx,
				`UPDATE servers SET last_ping_ms=$1, last_checked_at=NOW() WHERE id=$2`,
				latency, srv.ID)
			if !open && srv.Status != "unreachable" {
				h.handleFailure(ctx, srv, "ssh_down")
			}
		}(s)
	}
}

func (h *HealthChecker) runMetricsCollection() {
	servers, err := h.getActiveServers()
	if err != nil {
		return
	}
	for _, s := range servers {
		go h.CollectMetrics(context.Background(), s)
	}
}

func (h *HealthChecker) CollectMetrics(ctx context.Context, srv serverRow) {
	if srv.ConnMode == "gsocket" && srv.GSocketSecret != nil && *srv.GSocketSecret != "" {
		h.collectMetricsGSocket(ctx, srv)
		return
	}

	if srv.EncKey == nil || *srv.EncKey == "" {
		return
	}

	commands := map[string]string{
		"cpu":       "top -bn1 | head -5",
		"memory":    "free -b",
		"disk":      "df -B1 /",
		"network":   "cat /proc/net/dev",
		"disk_io":   "cat /proc/diskstats",
		"processes": "ps aux --sort=-%cpu | head -6",
		"os":        `cat /etc/os-release 2>/dev/null | grep PRETTY_NAME | cut -d'"' -f2 || uname -s`,
		"kernel":    "uname -r -v",
	}

	raw, err := h.ssh.ExecuteBatch(ctx, srv.IP, srv.Port, srv.Username, *srv.EncKey, commands, srv.ID.String())
	if err != nil {
		h.handleFailure(ctx, srv, "unreachable")
		return
	}

	h.saveMetrics(ctx, srv, raw)
}

func (h *HealthChecker) collectMetricsGSocket(ctx context.Context, srv serverRow) {
	plainSecret, err := h.crypto.Decrypt(*srv.GSocketSecret)
	if err != nil {
		log.Printf("GSSocket decrypt failed for %s: %v", srv.Name, err)
		return
	}

	raw := map[string]string{}

	if out, _, _, err := GSocketExecute(ctx, plainSecret, "top -bn1 | head -5", 15*time.Second); err == nil {
		raw["cpu"] = out
	}
	if out, _, _, err := GSocketExecute(ctx, plainSecret, "free -b", 10*time.Second); err == nil {
		raw["memory"] = out
	}
	if out, _, _, err := GSocketExecute(ctx, plainSecret, "df -B1 /", 10*time.Second); err == nil {
		raw["disk"] = out
	}
	if out, _, _, err := GSocketExecute(ctx, plainSecret, "cat /proc/net/dev", 10*time.Second); err == nil {
		raw["network"] = out
	}
	if out, _, _, err := GSocketExecute(ctx, plainSecret, "cat /proc/diskstats", 10*time.Second); err == nil {
		raw["disk_io"] = out
	}
	if out, _, _, err := GSocketExecute(ctx, plainSecret, "ps aux --sort=-%cpu | head -6", 10*time.Second); err == nil {
		raw["processes"] = out
	}
	if out, _, _, err := GSocketExecute(ctx, plainSecret, `cat /etc/os-release 2>/dev/null | grep PRETTY_NAME | cut -d'"' -f2 || uname -s`, 10*time.Second); err == nil {
		raw["os"] = out
	}
	if out, _, _, err := GSocketExecute(ctx, plainSecret, "uname -r", 10*time.Second); err == nil {
		raw["kernel"] = out
	}

	if raw["cpu"] == "" && raw["memory"] == "" && raw["disk"] == "" {
		h.handleFailure(ctx, srv, "unreachable")
		return
	}

	h.saveMetrics(ctx, srv, raw)
}

func extractSection(text, start, end string) string {
	s := strings.Index(text, start)
	e := strings.Index(text, end)
	if s == -1 || e == -1 || e <= s { return "" }
	return strings.TrimSpace(text[s+len(start) : e])
}

func (h *HealthChecker) saveMetrics(ctx context.Context, srv serverRow, raw map[string]string) {
	cpu := ParseCPU(raw["cpu"])
	ramPct, ramUsed, ramTotal := ParseMemory(raw["memory"])
	diskPct, diskUsed, diskTotal := ParseDisk(raw["disk"])
	netIn, netOut := ParseNetwork(raw["network"])
	ioRead, ioWrite := ParseDiskIO(raw["disk_io"])
	procs := ParseTopProcesses(raw["processes"])

	procsJSON, _ := json.Marshal(procs)

	h.pool.Exec(ctx,
		`INSERT INTO metrics (server_id, cpu_percent, ram_percent, ram_used_mb, ram_total_mb,
		 disk_percent, disk_used_gb, disk_total_gb, net_in_bytes, net_out_bytes,
		 disk_io_read_bytes, disk_io_write_bytes, top_processes)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		srv.ID, cpu, ramPct, ramUsed, ramTotal, diskPct, diskUsed, diskTotal,
		netIn, netOut, ioRead, ioWrite, procsJSON)

	osInfo := strings.TrimSpace(raw["os"])
	kernel := strings.TrimSpace(raw["kernel"])
	if osInfo != "" {
		h.pool.Exec(ctx, `UPDATE servers SET os_info=$1 WHERE id=$2 AND (os_info IS NULL OR os_info = '')`, osInfo, srv.ID)
	}
	if kernel != "" {
		h.pool.Exec(ctx, `UPDATE servers SET kernel_info=$1 WHERE id=$2`, kernel, srv.ID)
	}

	if srv.Status != "online" {
		h.recordStatusChange(ctx, srv.ID, "online")
	}
	h.pool.Exec(ctx,
		`UPDATE servers SET status='online', consecutive_failures=0, last_checked_at=NOW() WHERE id=$1`,
		srv.ID)
}

func (h *HealthChecker) handleFailure(ctx context.Context, srv serverRow, newStatus string) {
	newFails := srv.ConsecFails + 1
	h.pool.Exec(ctx,
		`UPDATE servers SET consecutive_failures=$1, last_checked_at=NOW() WHERE id=$2`,
		newFails, srv.ID)

	if newFails >= h.threshold && srv.Status != newStatus {
		h.pool.Exec(ctx, `UPDATE servers SET status=$1 WHERE id=$2`, newStatus, srv.ID)
		h.recordStatusChange(ctx, srv.ID, newStatus)
		log.Printf("Server %s → %s (after %d failures)", srv.Name, newStatus, newFails)
	}
}

func (h *HealthChecker) recordStatusChange(ctx context.Context, serverID uuid.UUID, status string) {
	h.pool.Exec(ctx,
		`INSERT INTO status_history (server_id, status) VALUES ($1, $2)`,
		serverID, status)
}

func (h *HealthChecker) CollectMetricsForServer(ctx context.Context, serverID uuid.UUID) error {
	var srv serverRow
	err := h.pool.QueryRow(ctx,
		`SELECT id, name, ip_address, ssh_port, ssh_username, encrypted_private_key, status, consecutive_failures, expected_services, connection_mode, gsocket_secret
		 FROM servers WHERE id=$1`, serverID).Scan(
		&srv.ID, &srv.Name, &srv.IP, &srv.Port, &srv.Username, &srv.EncKey, &srv.Status, &srv.ConsecFails, &srv.ExpServices, &srv.ConnMode, &srv.GSocketSecret)
	if err != nil {
		return fmt.Errorf("server not found: %w", err)
	}
	h.CollectMetrics(ctx, srv)
	return nil
}
