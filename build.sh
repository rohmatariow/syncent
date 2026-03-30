#!/bin/bash
set -e

echo "=== SynCent — Build ==="
echo ""

echo "Checking dependencies..."

# Go
if ! command -v go &>/dev/null; then
  echo "ERROR: Go not installed. Install: https://go.dev/dl/"
  exit 1
fi
echo "  Go: $(go version | awk '{print $3}')"

# Node
if ! command -v node &>/dev/null; then
  echo "ERROR: Node.js not installed. Install: https://nodejs.org/"
  exit 1
fi
echo "  Node: $(node -v)"

# npm
if ! command -v npm &>/dev/null; then
  echo "ERROR: npm not installed."
  exit 1
fi
echo "  npm: $(npm -v)"

if command -v gs-netcat &>/dev/null; then
  echo "  gs-netcat: installed"
else
  echo "  gs-netcat: not found (optional, needed for GSSocket)"
  echo ""
  echo "  Install GSSocket:"
  if [[ "$OSTYPE" == "darwin"* ]]; then
    echo "    brew install gsocket"
  elif [[ "$OSTYPE" == "linux"* ]]; then
    echo "    curl -sSL https://gsocket.io/install | bash"
    echo "    OR: download gs-netcat from https://github.com/hackerschoice/gsocket/releases"
  fi
  echo ""
  read -p "  Continue without GSSocket support? [Y/n] " -n 1 -r
  echo ""
  if [[ $REPLY =~ ^[Nn]$ ]]; then exit 1; fi
fi

echo ""

# ── Build frontend ──
echo "Building frontend..."
cd frontend
npm install --silent
npm run build
cd ..

# ── Build Go binary ──
echo "Building backend..."
go mod tidy
CGO_ENABLED=0 go build -ldflags="-s -w" -o syncent ./cmd/server

echo ""
echo "========================================="
echo "  Build complete: ./syncent"
echo "========================================="
echo ""
echo "  Quick start:"
echo "    1. Start PostgreSQL:  docker compose up -d"
echo "    2. Generate keys:     go run cmd/keygen/main.go"
echo "    3. Copy to .env:      cp .env.example .env  (paste keys)"
echo "    4. Run:                APP_ENV=development ./syncent"
echo "    5. Open:               http://localhost:8000"
echo ""
echo "  Docker (all-in-one):"
echo "    docker compose -f docker-compose.prod.yml up -d --build"
echo ""
