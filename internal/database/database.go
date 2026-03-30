package database

import (
	"context"
	"fmt"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"
)

var Pool *pgxpool.Pool

func Connect(databaseURL string) error {
	var err error
	Pool, err = pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		return fmt.Errorf("unable to connect to database: %w", err)
	}
	if err := Pool.Ping(context.Background()); err != nil {
		return fmt.Errorf("unable to ping database: %w", err)
	}
	log.Println("Database connected.")
	return nil
}

func Close() {
	if Pool != nil { Pool.Close() }
}

func Migrate() error {
	ctx := context.Background()

	tables := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			username VARCHAR(50) UNIQUE NOT NULL,
			password_hash VARCHAR(255) NOT NULL,
			totp_secret_encrypted TEXT,
			is_totp_enabled BOOLEAN DEFAULT false,
			role VARCHAR(20) DEFAULT 'user',
			failed_login_attempts INTEGER DEFAULT 0,
			locked_until TIMESTAMPTZ,
			refresh_token_hash VARCHAR(255),
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)`,

		`CREATE TABLE IF NOT EXISTS servers (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			user_id UUID REFERENCES users(id) ON DELETE CASCADE,
			name VARCHAR(100) NOT NULL,
			ip_address VARCHAR(45) NOT NULL,
			ssh_port INTEGER DEFAULT 22,
			ssh_username VARCHAR(50) NOT NULL,
			connection_mode VARCHAR(20) DEFAULT 'direct',
			encrypted_private_key TEXT,
			public_key TEXT,
			host_key_fingerprint VARCHAR(100),
			location VARCHAR(100),
			os_info VARCHAR(200),
			kernel_info VARCHAR(200),
			tags JSONB DEFAULT '[]',
			notes TEXT,
			expected_services JSONB DEFAULT '[]',
			status VARCHAR(20) DEFAULT 'pending_setup',
			last_ping_ms DOUBLE PRECISION,
			consecutive_failures INTEGER DEFAULT 0,
			health_check_interval INTEGER DEFAULT 30,
			log_paths JSONB DEFAULT '["/var/log/syslog"]',
			gsocket_secret TEXT,
			last_checked_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)`,

		`CREATE TABLE IF NOT EXISTS audit_log (
			id BIGSERIAL PRIMARY KEY,
			user_id UUID,
			server_id UUID,
			action VARCHAR(50) NOT NULL,
			detail TEXT,
			source_ip VARCHAR(45),
			prev_hash VARCHAR(64) NOT NULL,
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,

		`CREATE TABLE IF NOT EXISTS status_history (
			id BIGSERIAL PRIMARY KEY,
			server_id UUID NOT NULL,
			status VARCHAR(20) NOT NULL,
			changed_at TIMESTAMPTZ DEFAULT NOW()
		)`,

		`CREATE TABLE IF NOT EXISTS metrics (
			id BIGSERIAL PRIMARY KEY,
			server_id UUID NOT NULL,
			cpu_percent DOUBLE PRECISION,
			ram_percent DOUBLE PRECISION,
			ram_used_mb INTEGER,
			ram_total_mb INTEGER,
			disk_percent DOUBLE PRECISION,
			disk_used_gb DOUBLE PRECISION,
			disk_total_gb DOUBLE PRECISION,
			net_in_bytes BIGINT,
			net_out_bytes BIGINT,
			disk_io_read_bytes BIGINT,
			disk_io_write_bytes BIGINT,
			top_processes JSONB,
			recorded_at TIMESTAMPTZ DEFAULT NOW()
		)`,

		`CREATE TABLE IF NOT EXISTS snippets (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			user_id UUID REFERENCES users(id) ON DELETE CASCADE,
			name VARCHAR(100) NOT NULL,
			command TEXT NOT NULL,
			category VARCHAR(50) DEFAULT 'custom',
			is_dangerous BOOLEAN DEFAULT false,
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,

		`CREATE TABLE IF NOT EXISTS used_totp_codes (
			id BIGSERIAL PRIMARY KEY,
			username VARCHAR(50) NOT NULL,
			code VARCHAR(10) NOT NULL,
			used_at TIMESTAMPTZ DEFAULT NOW()
		)`,

		`CREATE INDEX IF NOT EXISTS idx_audit_server_time ON audit_log(server_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_user_time ON audit_log(user_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_time ON audit_log(created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_status_server_time ON status_history(server_id, changed_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_metrics_server_time ON metrics(server_id, recorded_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_used_totp ON used_totp_codes(username, code, used_at)`,

		`CREATE TABLE IF NOT EXISTS app_config (key VARCHAR(50) PRIMARY KEY, value TEXT)`,
		`CREATE INDEX IF NOT EXISTS idx_servers_user ON servers(user_id)`,

		`ALTER TABLE servers ADD COLUMN IF NOT EXISTS user_id UUID REFERENCES users(id) ON DELETE CASCADE`,
		`ALTER TABLE servers ADD COLUMN IF NOT EXISTS gsocket_secret TEXT`,
		`ALTER TABLE servers ADD COLUMN IF NOT EXISTS kernel_info VARCHAR(200)`,
		`ALTER TABLE servers ADD COLUMN IF NOT EXISTS host_key_fingerprint VARCHAR(100)`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS role VARCHAR(20) DEFAULT 'user'`,
		`ALTER TABLE audit_log ADD COLUMN IF NOT EXISTS user_id UUID`,
		`ALTER TABLE snippets ADD COLUMN IF NOT EXISTS user_id UUID REFERENCES users(id) ON DELETE CASCADE`,
	}

	for _, sql := range tables {
		if _, err := Pool.Exec(ctx, sql); err != nil {
			log.Printf("Migration note: %s (SQL: %.80s)", err.Error(), sql)
		}
	}

	log.Println("Database migrations complete.")
	return nil
}
