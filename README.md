![SynCent](/static/syncent.png)

# SynCent

One dashboard. All your servers. No agents.

SynCent is a self-hosted, open-source server monitoring and management dashboard built with Go and React. It connects to your servers via SSH or GSSocket (for servers behind NAT/firewall) without installing any agent on the target machine.

---

## Features

**Server Monitoring**
- Real-time CPU, RAM, disk, network, and disk I/O metrics
- Process monitoring (top 5 by CPU usage)
- OS and kernel detection
- Availability tracking (24h / 7d / 30d) with uptime percentage
- 3-layer health checks: ICMP ping (30s), TCP port (60s), full SSH/GSSocket metrics (5min)
- Status timeline and history

**Dual Connection Modes**
- Direct SSH with Ed25519 key authentication
- GSSocket for servers behind NAT, CGNAT, or firewalls (no public IP required)
- Automatic key generation and injection
- Per-server connection mode selection

**Remote Management**
- Interactive web terminal (SSH and GSSocket) with xterm.js
- Command execution with 3-tier safety classification (safe / dangerous / blocked)
- Live log streaming via WebSocket
- Command snippets library (user-scoped)

**Security**
- AES-256-GCM encryption at rest for all credentials
- Mandatory TOTP two-factor authentication for login
- Configurable 2FA for other actions (delete, dangerous commands)
- RBAC (admin / user roles)
- JWT authentication with short-lived access tokens (15min) and HttpOnly refresh cookies
- Account lockout after failed attempts
- HMAC-SHA256 audit trail with hash chain integrity verification
- bcrypt password hashing with SHA-256 pre-hash
- TOTP replay prevention
- Content Security Policy, X-Frame-Options, HSTS headers
- Security checklist that blocks startup with weak configuration

**Backup and Migration**
- Export/import server configurations as JSON
- Optional AES-256-GCM encrypted key export (PBKDF2 key derivation)
- Import preview with data validation and masked credentials
- Sample configuration downloads

**Multi-User**
- User registration with admin approval toggle
- Per-user server isolation (each user sees only their own servers)
- Admin panel for system configuration
- User-scoped audit logs and snippets

---

## Architecture

```
SynCent Host                     Target Servers
+------------------+
|  Go Backend      |---SSH--->       [Server with public IP]
|  (single binary) |
|                  |---GSSocket--->  [Server behind NAT/firewall]
|  React Frontend  |
|  (embedded)      |
|                  |
|  PostgreSQL      |
+------------------+
```

- **Backend**: Go with chi router, pgx database driver, gorilla/websocket
- **Frontend**: React with Tailwind CSS, xterm.js for terminal
- **Database**: PostgreSQL 16
- **Reverse Proxy**: Caddy (optional, for HTTPS)
- **GSSocket**: gs-netcat from hackerschoice/gsocket

The application compiles to a single binary that serves both the API and the embedded frontend. No separate web server required.

---

## Requirements

- Go 1.22+
- Node.js 20+
- PostgreSQL 16+
- Docker (for PostgreSQL and/or full deployment)
- gs-netcat (optional, for GSSocket server support)

---

## Quick Start

### Option 1: Automated Setup (Local Development)

```bash
git clone https://github.com/rohmatariow/syncent.git
cd syncent
chmod +x setup.sh
./setup.sh
APP_ENV=development ./syncent
```

`setup.sh` handles everything: starts PostgreSQL in Docker, generates cryptographic secrets, builds the frontend and backend, checks for optional dependencies.

Open `http://localhost:8000` and create your admin account.

### Option 2: Docker (Production)

```bash
git clone https://github.com/rohmatariow/syncent.git
cd syncent
cp .env.example .env

# Generate secrets
openssl rand -base64 32   # paste as MASTER_KEY in .env
openssl rand -base64 64   # paste as JWT_SECRET in .env

# Set a strong database password
# Edit .env: DB_PASSWORD=your-strong-password

docker compose -f docker-compose.prod.yml up -d --build
```

Open `http://localhost:8000` and create your admin account.

### Option 3: Manual Build

```bash
# 1. Start PostgreSQL
docker compose up -d

# 2. Generate secrets
cp .env.example .env
openssl rand -base64 32   # MASTER_KEY
openssl rand -base64 64   # JWT_SECRET
nano .env                 # paste secrets

# 3. Build frontend
cd frontend && npm install && npm run build && cd ..

# 4. Build backend
go mod tidy
CGO_ENABLED=0 go build -ldflags="-s -w" -o syncent ./cmd/server

# 5. Run
APP_ENV=development ./syncent
```

---

## Configuration

All configuration is via environment variables. Copy `.env.example` to `.env` and edit.

| Variable | Default | Description |
|----------|---------|-------------|
| `DATABASE_URL` | (required) | PostgreSQL connection string |
| `MASTER_KEY` | (required) | Base64-encoded 32-byte key for AES-256-GCM. Generate with `openssl rand -base64 32` |
| `JWT_SECRET` | (required) | JWT signing secret. Generate with `openssl rand -base64 64` |
| `APP_ENV` | `production` | `production` or `development` |
| `APP_PORT` | `8000` | HTTP listen port |
| `ENFORCE_HTTPS` | `true` | Block startup if false in production |
| `JWT_ACCESS_EXPIRE_MINUTES` | `15` | Access token lifetime |
| `JWT_REFRESH_EXPIRE_DAYS` | `7` | Refresh token lifetime |
| `SSH_CONNECT_TIMEOUT` | `10` | SSH connection timeout in seconds |
| `SSH_COMMAND_TIMEOUT` | `30` | SSH command timeout in seconds |
| `SSH_MAX_CONCURRENT_GLOBAL` | `30` | Max concurrent SSH connections |
| `SSH_MAX_CONCURRENT_PER_SERVER` | `2` | Max concurrent connections per server |
| `HEALTH_CHECK_CONSECUTIVE_FAILURES` | `3` | Failures before marking server unreachable |

**Critical**: Back up your `MASTER_KEY`. If lost, all encrypted server credentials become unrecoverable.

---

## Adding Servers

### SSH (Direct Connection)

1. Click "Add SSH Server" in the sidebar
2. Enter server name, IP address, SSH port, and username
3. Choose key setup method:
   - **Manual**: Copy the generated public key to the server's `~/.ssh/authorized_keys`
   - **Password Inject**: Enter SSH password once (not stored) to automatically install the key
4. Click "Test Connection"

### GSSocket (Behind NAT/Firewall)

For servers without a public IP address. Requires gs-netcat running on the target server.

1. On the target server, start gs-netcat:
   ```bash
   gs-netcat -l -s YOUR_SECRET -i
   ```
2. In SynCent, click "Add GSSocket Server"
3. Enter server name and the GSSocket secret
4. Click "Test Connection"

The SynCent host must also have gs-netcat installed. The Docker image includes it automatically.

---

## Project Structure

```
syncent/
|-- cmd/
|   |-- server/main.go          # Application entrypoint
|   |-- keygen/main.go          # Key generation utility
|-- internal/
|   |-- config/                 # Environment configuration and security checklist
|   |-- database/               # PostgreSQL connection and migrations
|   |-- handlers/               # HTTP handlers
|   |   |-- api.go              # Metrics, commands, snippets
|   |   |-- auth.go             # Login, setup, TOTP verification
|   |   |-- middleware.go       # Auth, admin middleware, security headers
|   |   |-- servers.go          # Server CRUD, key injection, connection testing
|   |   |-- settings.go         # App config, export/import, 2FA toggle
|   |   |-- history.go          # Audit log, status history, dashboard
|   |-- services/               # Business logic
|   |   |-- audit.go            # HMAC-SHA256 audit chain
|   |   |-- auth.go             # Password hashing, JWT, TOTP
|   |   |-- command_classifier.go
|   |   |-- crypto.go           # AES-256-GCM encryption, Ed25519 keys
|   |   |-- gsocket.go          # GSSocket command execution
|   |   |-- health_checker.go   # 3-layer health monitoring
|   |   |-- ssh_manager.go      # SSH connection management
|   |-- websocket/              # WebSocket handlers
|       |-- terminal.go         # SSH interactive terminal
|       |-- gs_terminal.go      # GSSocket terminal and log streaming
|       |-- logs.go             # SSH log streaming
|-- frontend/src/
|   |-- api/client.js           # API client
|   |-- components/             # React components
|   |-- pages/                  # Login and Dashboard pages
|-- Dockerfile                  # Multi-stage build
|-- docker-compose.yml          # Development (PostgreSQL only)
|-- docker-compose.prod.yml     # Production (full stack)
|-- setup.sh                    # Automated first-time setup
|-- build.sh                    # Manual build with dependency checks
|-- Caddyfile                   # Caddy reverse proxy configuration
```

---

## Security Model

### Encryption

- Server SSH private keys: AES-256-GCM with MASTER_KEY
- GSSocket secrets: AES-256-GCM with MASTER_KEY
- TOTP secrets: AES-256-GCM with MASTER_KEY
- Export backup keys: AES-256-GCM with PBKDF2-derived key (100,000 iterations)
- Passwords: bcrypt (cost 12) with SHA-256 pre-hash

### Authentication

- Login always requires username, password, and TOTP code
- The 2FA toggle only affects secondary actions (delete server, dangerous commands)
- Account lockout after 5 failed attempts with 15-minute cooldown
- Timing-equalized credential validation to prevent username enumeration
- TOTP replay prevention with 90-second tracking window

### Authorization

- Role-based access control with admin and user roles
- First user created via setup wizard is automatically admin
- Admin-only endpoints: app config, export/import, 2FA toggle, audit verification
- All server data scoped to owning user via user_id foreign key
- WebSocket connections verify server ownership before granting access

### Network

- Security headers: CSP, X-Frame-Options DENY, HSTS, Referrer-Policy, Permissions-Policy
- WebSocket origin validation
- HTTPS enforcement in production mode
- Request body size limit (10MB)
- JWT tokens stored in memory only (not localStorage)
- Refresh tokens via HttpOnly, Secure, SameSite=Strict cookies

### Audit

- Every action logged with HMAC-SHA256 hash chain
- Hash chain integrity verification endpoint
- User-scoped audit visibility
- Source IP tracking

---

## Command Classification

Commands executed via the dashboard are classified into three tiers:

| Tier | Examples | Requirement |
|------|----------|-------------|
| Safe | `uptime`, `df`, `ps aux`, `docker ps`, `journalctl` | None |
| Dangerous | `reboot`, `systemctl restart`, `apt install`, `kill`, `chmod` | TOTP or text confirmation |
| Blocked | `rm -rf /`, `mkfs`, `dd if=`, pipe to bash | Rejected entirely |

Unknown commands default to dangerous and require verification.

---

## GSSocket Setup

GSSocket enables connections to servers that do not have a public IP address.

### On the Target Server

```bash
# Install gsocket
bash -c "$(curl -fsSL gsocket.io/x)"

# Start listening (run in tmux or screen for persistence)
gs-netcat -l -s YOUR_SECRET_HERE -i
```

### On the SynCent Host (local development only)

```bash
# macOS
brew install gsocket

# Linux
bash -c "$(curl -fsSL gsocket.io/x)"
```

The Docker image includes gs-netcat automatically. No manual installation needed for Docker deployments.

---

## Production Deployment

### With Caddy (recommended)

1. Edit `Caddyfile` with your domain
2. Configure `.env` for production:
   ```
   APP_ENV=production
   ENFORCE_HTTPS=true
   DB_PASSWORD=strong-random-password
   ```
3. Deploy:
   ```bash
   docker compose -f docker-compose.prod.yml up -d --build
   ```

Caddy automatically provisions and renews TLS certificates via Let's Encrypt.

### Without Caddy

Place the application behind any reverse proxy (nginx, traefik, etc.) that terminates TLS. The application listens on port 8000 by default.

---

## Database

SynCent uses PostgreSQL. Migrations run automatically on every startup and are idempotent.

To reset the database:

```bash
docker exec -it syncent-go-db-1 psql -U syncent -d syncent \
  -c "DROP SCHEMA public CASCADE; CREATE SCHEMA public;"
```

---

## License

MIT License. See [LICENSE](LICENSE) for details.

---

## Acknowledgments

- [GSSocket](https://github.com/hackerschoice/gsocket) by hackerschoice for enabling connections to servers behind NAT
- [chi](https://github.com/go-chi/chi) for the lightweight Go HTTP router
- [xterm.js](https://xtermjs.org/) for the browser-based terminal emulator
- [pgx](https://github.com/jackc/pgx) for the PostgreSQL driver for Go
