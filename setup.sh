#!/bin/bash
set -e

echo "=== SynCent — First-Time Setup ==="
echo ""

# ── 1. Check Docker ──
if ! command -v docker &>/dev/null; then
  echo "ERROR: Docker not installed. Install: https://docs.docker.com/get-docker/"
  exit 1
fi
echo "[1/6] Docker: OK"

# ── 2. Start PostgreSQL ──
echo "[2/6] Starting PostgreSQL..."
EXISTING=$(docker ps -aq -f name=syncent-db 2>/dev/null)
if [ -n "$EXISTING" ]; then
  RUNNING=$(docker ps -q -f name=syncent-db 2>/dev/null)
  if [ -n "$RUNNING" ]; then
    echo "  PostgreSQL already running"
  else
    docker rm -f $EXISTING >/dev/null 2>&1 || true
    docker compose up -d
  fi
else
  docker compose up -d
fi
echo "  Waiting for PostgreSQL..."
for i in $(seq 1 30); do
  if docker compose exec -T db pg_isready -U syncent >/dev/null 2>&1; then echo "  PostgreSQL: ready"; break; fi
  if [ $i -eq 30 ]; then echo "  ERROR: PostgreSQL not ready"; exit 1; fi
  sleep 1
done

# ── 3. Generate .env ──
if [ ! -f .env ]; then
  echo "[3/6] Generating secrets..."
  cp .env.example .env
  MASTER_KEY=$(openssl rand -base64 32)
  JWT_SECRET=$(openssl rand -base64 64)
  if [[ "$OSTYPE" == "darwin"* ]]; then
    sed -i '' "s|^MASTER_KEY=.*|MASTER_KEY=$MASTER_KEY|" .env
    sed -i '' "s|^JWT_SECRET=.*|JWT_SECRET=$JWT_SECRET|" .env
  else
    sed -i "s|^MASTER_KEY=.*|MASTER_KEY=$MASTER_KEY|" .env
    sed -i "s|^JWT_SECRET=.*|JWT_SECRET=$JWT_SECRET|" .env
  fi
  echo "  .env created"
  echo ""
  echo "  !! BACK UP YOUR MASTER_KEY !!"
  echo "  MASTER_KEY=$MASTER_KEY"
  echo ""
else
  echo "[3/6] .env exists, skipping"
fi

# ── 4. Build frontend ──
echo "[4/6] Building frontend..."
cd frontend
npm install --silent
npm run build
cd ..

# ── 5. Build Go binary ──
echo "[5/6] Building backend..."
go mod tidy 2>/dev/null || true
CGO_ENABLED=0 go build -ldflags="-s -w" -o syncent ./cmd/server

# ── 6. Check gs-netcat ──
echo "[6/6] Checking gs-netcat..."
if command -v gs-netcat &>/dev/null; then
  echo "  gs-netcat: installed"
else
  echo "  gs-netcat: not found (GSSocket disabled)"
  echo ""
  echo "  Install gsocket:"
  echo "    curl -sSL https://github.com/hackerschoice/gsocket/releases/latest/download/gsocket-1.4.43.tar.gz -o gsocket.tar.gz"
  echo "    tar xfz gsocket.tar.gz"
  echo "    cd gsocket-*"
  echo "    ./configure && make && sudo make install"
  echo ""
  read -p "  Continue without GSSocket? [Y/n] " -n 1 -r
  echo ""
  if [[ $REPLY =~ ^[Nn]$ ]]; then exit 1; fi
fi

echo ""
echo "========================================="
echo "  Setup complete!"
echo "========================================="
echo ""
echo "  Run:   APP_ENV=development ./syncent"
echo "  Open:  http://localhost:8000"
echo ""
