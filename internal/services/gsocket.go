package services

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\][^\x07]*\x07|\[\?[0-9]+[hl]`)

func StripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }

func GSocketExecute(ctx context.Context, secret, command string, timeout time.Duration) (string, string, int, error) {
	if timeout == 0 { timeout = 30 * time.Second }
	if len(command) > 4096 {
		return "", "", -1, fmt.Errorf("command too long (max 4096 bytes)")
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	markerID := randomHex(8)
	startM := "VPCMD_S_" + markerID
	endM := "VPCMD_E_" + markerID
	command = strings.ReplaceAll(command, "VPCMD_S_", "")
	command = strings.ReplaceAll(command, "VPCMD_E_", "")

	script := fmt.Sprintf(
		"export TERM=dumb 2>/dev/null\n"+
			"unset PS1 2>/dev/null\n"+
			"stty -echo 2>/dev/null\n"+
			"echo '%s'\n"+
			"%s\n"+
			"echo '%s'\"$?\"\n"+
			"exit\n",
		startM, command, endM)

	cmd := exec.CommandContext(ctx, "gs-netcat", "-s", secret, "-i")
	cmd.Stdin = strings.NewReader(script)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Run()

	if ctx.Err() != nil {
		return "", "", -1, fmt.Errorf("timed out after %s", timeout)
	}

	raw := StripANSI(stdout.String())

	si := strings.Index(raw, startM)
	if si == -1 {
		if strings.Contains(stderr.String(), "takes longer") || strings.Contains(stderr.String(), "Connecting") {
			return "", "", -1, fmt.Errorf("GSSocket timed out. Check gs-netcat is running on server")
		}
		return "", "", -1, fmt.Errorf("GSSocket connection failed")
	}

	content := raw[si+len(startM):]
	ei := strings.Index(content, endM)
	exitCode := 0
	var output string

	if ei != -1 {
		output = content[:ei]
		after := content[ei+len(endM):]
		fmt.Sscanf(strings.TrimSpace(strings.Split(after, "\n")[0]), "%d", &exitCode)
	} else {
		output = content
	}

	lines := strings.Split(output, "\n")
	var clean []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" { continue }
		if trimmed == "export TERM=dumb" { continue }
		if trimmed == "unset PS1" { continue }
		if strings.HasPrefix(trimmed, "stty ") { continue }
		if trimmed == fmt.Sprintf("echo '%s'", startM) { continue }
		if strings.HasPrefix(trimmed, fmt.Sprintf("echo '%s'", endM)) { continue }
		if trimmed == command { continue }
		if trimmed == "exit" { continue }
		// Skip shell prompts (user@host patterns)
		if isPromptLine(trimmed) { continue }
		clean = append(clean, line)
	}

	return strings.TrimSpace(strings.Join(clean, "\n")), "", exitCode, nil
}

func isPromptLine(line string) bool {
	if regexp.MustCompile(`^[\w.-]+@[\w.-]+[:\s~/$#]+$`).MatchString(line) { return true }
	if regexp.MustCompile(`^\$\s*$`).MatchString(line) { return true }
	return false
}

func GSocketTestConnection(secret string, timeout time.Duration) (bool, string, string, string, error) {
	if timeout == 0 { timeout = 20 * time.Second }
	ctx := context.Background()

	cmd := `echo "OS:$(cat /etc/os-release 2>/dev/null | grep PRETTY_NAME | cut -d'"' -f2 || uname -s)" && echo "USER:$(whoami)" && echo "KERNEL:$(uname -r)"`

	stdout, _, _, err := GSocketExecute(ctx, secret, cmd, timeout)
	if err != nil { return false, "", "", "", err }

	osInfo, username, kernel := "Linux", "unknown", ""
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "OS:") { osInfo = strings.TrimPrefix(line, "OS:") }
		if strings.HasPrefix(line, "USER:") { username = strings.TrimPrefix(line, "USER:") }
		if strings.HasPrefix(line, "KERNEL:") { kernel = strings.TrimPrefix(line, "KERNEL:") }
	}

	return true, osInfo, username, kernel, nil
}

func IsGSocketAvailable() bool {
	_, err := exec.LookPath("gs-netcat")
	return err == nil
}
