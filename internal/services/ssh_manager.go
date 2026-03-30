package services

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

type SSHManager struct {
	crypto         *CryptoService
	connectTimeout time.Duration
	commandTimeout time.Duration
	globalSem      chan struct{}
	serverSems     map[string]chan struct{}
	serverMu       sync.Mutex
	maxPerServer   int
}

func NewSSHManager(crypto *CryptoService, maxGlobal, maxPerServer, connectTimeout, commandTimeout int) *SSHManager {
	return &SSHManager{
		crypto:         crypto,
		connectTimeout: time.Duration(connectTimeout) * time.Second,
		commandTimeout: time.Duration(commandTimeout) * time.Second,
		globalSem:      make(chan struct{}, maxGlobal),
		serverSems:     make(map[string]chan struct{}),
		maxPerServer:   maxPerServer,
	}
}

func (m *SSHManager) getServerSem(serverID string) chan struct{} {
	m.serverMu.Lock()
	defer m.serverMu.Unlock()
	if _, ok := m.serverSems[serverID]; !ok {
		m.serverSems[serverID] = make(chan struct{}, m.maxPerServer)
	}
	return m.serverSems[serverID]
}

func (m *SSHManager) connect(ip string, port int, username string, signer ssh.Signer) (*ssh.Client, error) {
	config := &ssh.ClientConfig{
		User:            username,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         m.connectTimeout,
	}
	addr := fmt.Sprintf("%s:%d", ip, port)
	return ssh.Dial("tcp", addr, config)
}

type CommandResult struct {
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	ExitCode   int    `json:"exit_code"`
	DurationMs int64  `json:"duration_ms"`
}

func (m *SSHManager) ExecuteCommand(ctx context.Context, ip string, port int, username, encryptedKey, command, serverID string, timeout time.Duration) (*CommandResult, error) {
	if timeout == 0 {
		timeout = m.commandTimeout
	}

	m.globalSem <- struct{}{}
	defer func() { <-m.globalSem }()

	srvSem := m.getServerSem(serverID)
	srvSem <- struct{}{}
	defer func() { <-srvSem }()

	signer, err := m.crypto.ParsePrivateKey(encryptedKey)
	if err != nil {
		return nil, fmt.Errorf("key error: %w", err)
	}

	start := time.Now()

	client, err := m.connect(ip, port, username, signer)
	if err != nil {
		return nil, fmt.Errorf("SSH connect: %w", err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("SSH session: %w", err)
	}
	defer session.Close()

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	done := make(chan error, 1)
	go func() {
		done <- session.Run(command)
	}()

	select {
	case err := <-done:
		duration := time.Since(start).Milliseconds()
		exitCode := 0
		if err != nil {
			if exitErr, ok := err.(*ssh.ExitError); ok {
				exitCode = exitErr.ExitStatus()
			} else {
				return nil, err
			}
		}
		return &CommandResult{
			Stdout:     stdout.String(),
			Stderr:     stderr.String(),
			ExitCode:   exitCode,
			DurationMs: duration,
		}, nil
	case <-time.After(timeout):
		session.Signal(ssh.SIGTERM)
		return &CommandResult{
			Stdout:     stdout.String(),
			Stderr:     fmt.Sprintf("Command timed out after %v", timeout),
			ExitCode:   -1,
			DurationMs: time.Since(start).Milliseconds(),
		}, nil
	}
}

func (m *SSHManager) ExecuteBatch(ctx context.Context, ip string, port int, username, encryptedKey string, commands map[string]string, serverID string) (map[string]string, error) {
	m.globalSem <- struct{}{}
	defer func() { <-m.globalSem }()

	srvSem := m.getServerSem(serverID)
	srvSem <- struct{}{}
	defer func() { <-srvSem }()

	signer, err := m.crypto.ParsePrivateKey(encryptedKey)
	if err != nil {
		return nil, err
	}

	client, err := m.connect(ip, port, username, signer)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	results := make(map[string]string)
	for label, cmd := range commands {
		session, err := client.NewSession()
		if err != nil {
			results[label] = ""
			continue
		}
		var out bytes.Buffer
		session.Stdout = &out

		done := make(chan error, 1)
		go func() { done <- session.Run(cmd) }()

		select {
		case <-done:
			results[label] = out.String()
		case <-time.After(15 * time.Second):
			results[label] = ""
		}
		session.Close()
	}
	return results, nil
}

func (m *SSHManager) ConnectWithPassword(ip string, port int, username, password string) (*ssh.Client, error) {
	config := &ssh.ClientConfig{
		User:            username,
		Auth:            []ssh.AuthMethod{ssh.Password(password)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         m.connectTimeout,
	}
	return ssh.Dial("tcp", fmt.Sprintf("%s:%d", ip, port), config)
}

func (m *SSHManager) PingTCP(ip string, port int) (bool, float64) {
	start := time.Now()
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", ip, port), 5*time.Second)
	latency := float64(time.Since(start).Milliseconds())
	if err != nil {
		return false, latency
	}
	conn.Close()
	return true, latency
}

func (m *SSHManager) PingICMP(ip string) (bool, float64) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ping", "-c", "1", "-W", "2", ip)
	out, err := cmd.Output()
	latency := float64(time.Since(start).Milliseconds())
	if err != nil {
		return false, latency
	}

	re := regexp.MustCompile(`time[=<](\d+\.?\d*)`)
	if matches := re.FindStringSubmatch(string(out)); len(matches) > 1 {
		if _, err := fmt.Sscanf(matches[1], "%f", &latency); err == nil {
			return true, latency
		}
	}
	return true, latency
}

func RunOnClient(client *ssh.Client, command string) (string, error) {
	session, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()

	var out bytes.Buffer
	session.Stdout = &out
	if err := session.Run(command); err != nil {
		if _, ok := err.(*ssh.ExitError); !ok {
			return out.String(), err
		}
	}
	return strings.TrimSpace(out.String()), nil
}

type GSocketTunnel struct {
	cmd       *exec.Cmd
	localPort int
	cancel    context.CancelFunc
}

func GenerateGSocketSecret() string {
	b := make([]byte, 16)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}

func IsGSocketInstalled() bool {
	_, err := exec.LookPath("gs-netcat")
	return err == nil
}

func (m *SSHManager) StartGSocketTunnel(gsocketSecret string) (*GSocketTunnel, error) {
	if !IsGSocketInstalled() {
		return nil, fmt.Errorf("gs-netcat not installed. Install: https://gsocket.io/install")
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("no free port: %w", err)
	}
	localPort := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	ctx, cancel := context.WithCancel(context.Background())

	cmd := exec.CommandContext(ctx, "gs-netcat", "-s", gsocketSecret, "-p", fmt.Sprintf("%d", localPort))
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to start gs-netcat: %w", err)
	}

	time.Sleep(2 * time.Second)

	return &GSocketTunnel{
		cmd:       cmd,
		localPort: localPort,
		cancel:    cancel,
	}, nil
}

func (m *SSHManager) ConnectViaGSocket(gsocketSecret, username string, signer ssh.Signer) (*ssh.Client, *GSocketTunnel, error) {
	tunnel, err := m.StartGSocketTunnel(gsocketSecret)
	if err != nil {
		return nil, nil, err
	}

	config := &ssh.ClientConfig{
		User:            username,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         m.connectTimeout,
	}

	client, err := ssh.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", tunnel.localPort), config)
	if err != nil {
		tunnel.Close()
		return nil, nil, fmt.Errorf("SSH via gsocket failed: %w", err)
	}

	return client, tunnel, nil
}

func (t *GSocketTunnel) Close() {
	if t.cancel != nil {
		t.cancel()
	}
	if t.cmd != nil && t.cmd.Process != nil {
		t.cmd.Process.Kill()
		t.cmd.Wait()
	}
}
