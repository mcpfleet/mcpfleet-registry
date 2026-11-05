<div align="center">

# mcpfleet-registry

**REST API backend for mcpfleet — stores MCP server definitions and manages auth tokens**

[![CI](https://github.com/mcpfleet/mcpfleet-registry/actions/workflows/ci.yml/badge.svg)](https://github.com/mcpfleet/mcpfleet-registry/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/go-1.22-blue)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

[CLI tool](https://github.com/mcpfleet/mcpfleet) · [API docs (Swagger)](#api-docs) · [Report a bug](https://github.com/mcpfleet/mcpfleet-registry/issues)

</div>

---

## Overview

`mcpfleet-registry` is the server-side component of the mcpfleet ecosystem. It provides a REST API for storing, retrieving, and managing MCP server definitions. It's designed to be self-hosted — run it on your own machine or VPS.

## Stack

| Component | Technology |
|-----------|------------|
| Language | Go 1.22 |
| HTTP framework | [Huma v2](https://huma.rocks/) (OpenAPI 3.1, auto-docs) |
| Router | [chi v5](https://github.com/go-chi/chi) |
| Database | SQLite (WAL mode, zero external dependencies) |
| Migrations | [golang-migrate](https://github.com/golang-migrate/migrate) |
| Container | Multi-stage Docker build (~20 MB final image) |

## Quick start

### Docker Compose (recommended)

```bash
git clone https://github.com/mcpfleet/mcpfleet-registry
cd mcpfleet-registry
docker compose up -d
```

The API will be available at `http://localhost:8080`.

### Manual

The SQLite driver needs cgo: a C compiler and the SQLite headers (for example `gcc` and `libsqlite3-dev` on Debian/Ubuntu).

```bash
git clone https://github.com/mcpfleet/mcpfleet-registry
cd mcpfleet-registry
CGO_ENABLED=1 go run ./cmd/registry
```

The database is created as `./registry.db` in the current directory; see [Configuration](#configuration) to change it.

## First-time setup

The registry includes a bootstrap endpoint to create your first admin token without requiring authentication. This endpoint only works when no tokens exist in the database.

```bash
# Create the first admin token (run once on first startup)
curl -X POST http://localhost:8080/bootstrap \
  -H 'Content-Type: application/json' \
  -d '{"name": "admin"}'
# => {"$schema":"...","id":"...","name":"admin","token":"mcp_...","created_at":"..."}
```

The request body and the response are plain JSON objects. A second call returns `403` once any token exists.

**Important:** Save the token immediately. The registry stores only its hash, so this response is the only time you see the full token.

Then log in with the mcpfleet CLI. It saves the token and the registry URL for later commands:

```bash
mcpfleet auth login --url http://localhost:8080 --token mcp_...
```

Run `mcpfleet auth login` without flags to enter both in an interactive form instead.

## Authentication

All `/v1/*` endpoints require a Bearer token:

```
Authorization: Bearer mcp_<token>
```

**Public paths** (no auth required):
- `GET /healthz` — health check
- `GET /docs` — Swagger UI
- `GET /openapi.json` — OpenAPI schema
- `POST /bootstrap` — Create first admin token (only works when database is empty)

## API reference

### Servers

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/servers` | List all MCP servers |
| `GET` | `/v1/servers/{name}` | Get a server by name |
| `POST` | `/v1/servers` | Create a new server |
| `PUT` | `/v1/servers/{name}` | Update an existing server |
| `DELETE` | `/v1/servers/{name}` | Delete a server |

### Auth tokens

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/tokens` | List all tokens |
| `POST` | `/v1/tokens` | Create a new token |
| `DELETE` | `/v1/tokens/{id}` | Revoke a token |

## API docs

When running, interactive API docs are available at:
- **Swagger UI**: `http://localhost:8080/docs`
- **OpenAPI JSON**: `http://localhost:8080/openapi.json`

## Configuration

| Environment variable | Default | Description |
|----------------------|---------|-------------|
| `DATABASE_URL` | `./registry.db` | Path to the SQLite database file (the Docker image and `docker-compose.yml` set `/data/registry.db`) |
| `PORT` | `8080` | HTTP listen port |

These are the only settings; the registry reads no other environment variables or config files.

## Development

```bash
git clone https://github.com/mcpfleet/mcpfleet-registry
cd mcpfleet-registry
CGO_ENABLED=1 go vet ./...
CGO_ENABLED=1 go test ./... -count=1
CGO_ENABLED=1 go run ./cmd/registry
```

## Docker

```bash
# Build image
docker build -t mcpfleet-registry .

# Run with a persistent database in a named volume
docker run -d \
  -p 8080:8080 \
  -v mcpfleet-registry-data:/data \
  -e DATABASE_URL=/data/registry.db \
  mcpfleet-registry
```

## Contributing

Pull requests are welcome! Please open an issue first to discuss major changes.

## License

[MIT](LICENSE)
