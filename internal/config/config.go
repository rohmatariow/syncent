package config

import (
	"crypto/aes"
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	DatabaseURL string
	MasterKey   []byte
	JWTSecret   string
	AppEnv      string
	AppHost     string
	AppPort     string

	JWTAccessExpireMin  int
	JWTRefreshExpireDays int
	EnforceHTTPS        bool

	SSHConnectTimeout       int
	SSHCommandTimeout       int
	SSHMaxConcurrentGlobal  int
	SSHMaxConcurrentPerSvr  int

	HealthCheckFailures int
	StaleServerDays     int
}

func Load() *Config {
	_ = godotenv.Load()

	cfg := &Config{
		DatabaseURL:            getEnv("DATABASE_URL", ""),
		JWTSecret:              getEnv("JWT_SECRET", ""),
		AppEnv:                 getEnv("APP_ENV", "production"),
		AppHost:                getEnv("APP_HOST", "0.0.0.0"),
		AppPort:                getEnv("APP_PORT", "8000"),
		JWTAccessExpireMin:     getEnvInt("JWT_ACCESS_EXPIRE_MINUTES", 15),
		JWTRefreshExpireDays:   getEnvInt("JWT_REFRESH_EXPIRE_DAYS", 7),
		EnforceHTTPS:           getEnvBool("ENFORCE_HTTPS", true),
		SSHConnectTimeout:      getEnvInt("SSH_CONNECT_TIMEOUT", 10),
		SSHCommandTimeout:      getEnvInt("SSH_COMMAND_TIMEOUT", 30),
		SSHMaxConcurrentGlobal: getEnvInt("SSH_MAX_CONCURRENT_GLOBAL", 30),
		SSHMaxConcurrentPerSvr: getEnvInt("SSH_MAX_CONCURRENT_PER_SERVER", 2),
		HealthCheckFailures:    getEnvInt("HEALTH_CHECK_CONSECUTIVE_FAILURES", 3),
		StaleServerDays:        getEnvInt("STALE_SERVER_DAYS", 7),
	}

	masterKeyStr := getEnv("MASTER_KEY", "")
	if masterKeyStr != "" {
		decoded, err := base64.StdEncoding.DecodeString(masterKeyStr)
		if err == nil && len(decoded) == 32 {
			cfg.MasterKey = decoded
		}
	}

	return cfg
}

func (c *Config) IsDev() bool {
	return c.AppEnv == "development"
}

func (c *Config) RunSecurityChecklist() {
	var errors []string
	var warnings []string

	insecure := []string{"changeme", "secret", "password", "default", "test", "xxx"}

	if len(c.MasterKey) == 0 {
		errors = append(errors,
			"MASTER_KEY is not set or invalid.\n"+
				"  Generate: go run cmd/keygen/main.go\n"+
				"  Or: openssl rand -base64 32\n"+
				"  Must be a base64-encoded 32-byte key for AES-256.")
	} else if _, err := aes.NewCipher(c.MasterKey); err != nil {
		errors = append(errors, "MASTER_KEY is not a valid AES-256 key.")
	}

	if c.JWTSecret == "" {
		errors = append(errors,
			"JWT_SECRET is not set.\n"+
				"  Generate: openssl rand -base64 64")
	} else {
		for _, v := range insecure {
			if strings.EqualFold(c.JWTSecret, v) {
				errors = append(errors, "JWT_SECRET is set to an insecure default.")
				break
			}
		}
		if len(c.JWTSecret) < 32 {
			errors = append(errors, "JWT_SECRET too short (min 32 chars).")
		}
	}

	if c.DatabaseURL == "" {
		errors = append(errors, "DATABASE_URL is not set.")
	} else if strings.Contains(c.DatabaseURL, "changeme") {
		if !c.IsDev() {
			errors = append(errors, "DATABASE_URL has default password. Change for production.")
		} else {
			warnings = append(warnings, "DATABASE_URL has default password. OK for dev only.")
		}
	}

	if !c.IsDev() && !c.EnforceHTTPS {
		errors = append(errors, "ENFORCE_HTTPS is false in production. SSH credentials need HTTPS.")
	}

	if c.IsDev() && !c.EnforceHTTPS {
		warnings = append(warnings, "HTTPS disabled. Only acceptable for local development.")
	}

	if len(warnings) > 0 {
		fmt.Println("\n" + strings.Repeat("=", 60))
		fmt.Println("⚠  SECURITY WARNINGS")
		fmt.Println(strings.Repeat("=", 60))
		for i, w := range warnings {
			fmt.Printf("\n  %d. %s\n", i+1, w)
		}
	}

	if len(errors) > 0 {
		fmt.Println("\n" + strings.Repeat("=", 60))
		fmt.Println("✖  SECURITY CHECK FAILED — CANNOT START")
		fmt.Println(strings.Repeat("=", 60))
		for i, e := range errors {
			fmt.Printf("\n  %d. %s\n", i+1, e)
		}
		fmt.Println("\n" + strings.Repeat("=", 60))
		log.Fatal("Fix security issues and restart.")
	}

	fmt.Println("\n✔  Security checklist passed.")
	
	if c.IsDev() {
		fmt.Println("   Running in DEVELOPMENT mode.\n")
	} else {
		fmt.Println("   Running in PRODUCTION mode.\n")
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}
