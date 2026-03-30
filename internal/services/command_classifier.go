package services

import (
	"regexp"
	"strings"
)

type CommandTier string

const (
	TierSafe      CommandTier = "safe"
	TierDangerous CommandTier = "dangerous"
	TierBlocked   CommandTier = "blocked"
)

var blockedPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\brm\s+(-[a-zA-Z]*f[a-zA-Z]*\s+)?/\s*$`),
	regexp.MustCompile(`(?i)\brm\s+-[a-zA-Z]*r[a-zA-Z]*\s+/\s*$`),
	regexp.MustCompile(`(?i)\bmkfs\b`),
	regexp.MustCompile(`(?i)\bdd\s+if=`),
	regexp.MustCompile(`(?i)>\s*/dev/[hs]d`),
	regexp.MustCompile(`(?i)curl\s+.*\|\s*(bash|sh|zsh|sudo)`),
	regexp.MustCompile(`(?i)wget\s+.*\|\s*(bash|sh|zsh|sudo)`),
	regexp.MustCompile(`:\(\)\{\s*:\|:&\s*\};:`),
}

var dangerousPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\breboot\b`),
	regexp.MustCompile(`(?i)\bshutdown\b`),
	regexp.MustCompile(`(?i)\bsystemctl\s+(restart|stop|start|enable|disable)\b`),
	regexp.MustCompile(`(?i)\bservice\s+\w+\s+(restart|stop|start)\b`),
	regexp.MustCompile(`(?i)\bapt(-get)?\s+(install|remove|purge|upgrade|dist-upgrade)\b`),
	regexp.MustCompile(`(?i)\byum\s+(install|remove|update|upgrade)\b`),
	regexp.MustCompile(`(?i)\bdnf\s+(install|remove|update|upgrade)\b`),
	regexp.MustCompile(`(?i)\bpacman\s+-[SRU]`),
	regexp.MustCompile(`(?i)\bdocker\s+(restart|stop|rm|kill|prune)\b`),
	regexp.MustCompile(`(?i)\bdocker\s+compose\s+(down|restart|stop)\b`),
	regexp.MustCompile(`(?i)\bkill\b`),
	regexp.MustCompile(`(?i)\bpkill\b`),
	regexp.MustCompile(`(?i)\bkillall\b`),
	regexp.MustCompile(`(?i)\bnginx\s+-s\b`),
	regexp.MustCompile(`(?i)\bufw\s+(enable|disable|allow|deny|delete)\b`),
	regexp.MustCompile(`(?i)\biptables\b`),
	regexp.MustCompile(`(?i)\bchmod\b`),
	regexp.MustCompile(`(?i)\bchown\b`),
	regexp.MustCompile(`(?i)\buseradd\b`),
	regexp.MustCompile(`(?i)\buserdel\b`),
	regexp.MustCompile(`(?i)\bpasswd\b`),
}

var safeCommands = []string{
	"uptime", "whoami", "hostname", "date", "id", "uname",
	"df", "free", "top", "htop", "vmstat", "iostat",
	"cat", "head", "tail", "less", "more", "wc", "grep",
	"ls", "ll", "pwd", "find", "which", "whereis",
	"ps", "pgrep", "lsof",
	"systemctl status", "systemctl is-active", "systemctl list-units",
	"ip addr", "ip route", "ifconfig", "ss", "netstat",
	"ping", "traceroute", "dig", "nslookup",
	"docker ps", "docker stats", "docker logs", "docker images",
	"docker compose ps", "docker compose logs",
	"timedatectl", "hostnamectl",
	"last", "w", "who",
	"journalctl", "dmesg", "vnstat",
}

func ClassifyCommand(command string) CommandTier {
	cmd := strings.TrimSpace(command)
	if cmd == "" {
		return TierBlocked
	}

	for _, p := range blockedPatterns {
		if p.MatchString(cmd) {
			return TierBlocked
		}
	}

	for _, p := range dangerousPatterns {
		if p.MatchString(cmd) {
			return TierDangerous
		}
	}

	for _, safe := range safeCommands {
		if cmd == safe || strings.HasPrefix(cmd, safe+" ") {
			return TierSafe
		}
	}

	if regexp.MustCompile(`\|\s*(bash|sh|zsh|sudo)`).MatchString(cmd) {
		return TierDangerous
	}

	return TierDangerous
}
