# SynCent — Dockerfile

# ── Stage 1: Build frontend ──
FROM --platform=linux/amd64 node:20-alpine AS frontend
WORKDIR /app
COPY frontend/package*.json ./
RUN npm install --silent
COPY frontend/ .
RUN npm run build

# ── Stage 2: Build Go binary ──
FROM --platform=linux/amd64 golang:1.22-alpine AS backend
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o syncent ./cmd/server

# ── Stage 3: Get GSocket ──
FROM --platform=linux/amd64 hackerschoice/gsocket:latest AS gsocket

# ── Stage 4: Runtime (Debian 11 AMD64 untuk kompatibilitas libssl1.1) ──
FROM --platform=linux/amd64 debian:bullseye-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates tzdata iputils-ping \
    curl bash openssh-client libssl1.1 \
    && apt-get clean && rm -rf /var/lib/apt/lists/*

# Get Gsocket Intel
COPY --from=gsocket /usr/bin/gs-netcat /usr/local/bin/gs-netcat
COPY --from=gsocket /usr/bin/gs-sftp /usr/local/bin/gs-sftp
COPY --from=gsocket /usr/bin/blitz /usr/local/bin/blitz

RUN useradd -m -d /app syncent
WORKDIR /app

# Get Go binary
COPY --from=backend --chown=syncent:syncent /build/syncent .

# Get frontend file
COPY --from=frontend --chown=syncent:syncent /app/dist ./frontend/dist

USER syncent
EXPOSE 8000

HEALTHCHECK --interval=30s --timeout=5s --retries=3 \
    CMD curl -f http://localhost:8000/api/health || exit 1

ENTRYPOINT ["./syncent"]