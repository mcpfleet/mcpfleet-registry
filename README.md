# registry

REST API backend for [mcpfleet](https://github.com/mcpfleet/mcpfleet) — stores MCP server definitions, manages auth tokens.

## Stack

- **Go 1.22** + **[Huma v2](https://huma.rocks/)** (OpenAPI 3.1, auto-docs)
- **SQLite** (WAL mode, zero external dependencies)
- **chi** router
- Multi-stage **Docker** build (~20 MB final image)

## Authentication

All `/v1/*` endpoints require a Bearer token:

```
Authorization: Bearer mcp_<token>
```

**Public paths** (no auth required):
- `GET /healthz` — health check
- `GET /docs` — Swagger UI
- `GET /openapi.json` — OpenAPI schema

**First-time setup** — bootstrap your first token directly on the server:

```bash
# Before auth is enabled, create the first token
curl -X POST http://localhost:8080/v1/tokens \
  -H 'Content-Type: application/json' \
  -d '{"name":"admin"}'

# Response (save the token — shown only once):
# {"id":"...","name":"admin","token":"mcp_..."}
```

After the first token exists, all subsequent requests require `Authorization: Bearer mcp_...`.

> **Note:** If you need to bootstrap with auth already enforced, set `BOOTSTRAP_TOKEN` env (see below).

## API

| Method | Path | Description |
|--------|------|-------------|
| GET | `/v1/servers` | List all MCP servers |
| POST | `/v1/servers` | Create MCP server |
| GET | `/v1/servers/{id}` | Get server by ID |
| PUT | `/v1/servers/{id}` | Update server |
| DELETE | `/v1/servers/{id}` | Delete server |
| GET | `/v1/tokens` | List auth tokens |
| POST | `/v1/tokens` | Create auth token |
| DELETE | `/v1/tokens/{id}` | Delete token |

Interactive docs at `/docs` when running.

## Running

### Docker Compose (recommended for VPS)

```bash
docker compose up -d
```

Data persists in a named volume `registry-data`. Override port:

```bash
PORT=9000 docker compose up -d
```

### Local (requires Go 1.22+ and CGO)

```bash
go run ./cmd/registry
```

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8080` | Listen port |
| `DATABASE_URL` | `./registry.db` | SQLite file path |

## Server Object

```json
{
  "id": "uuid",
  "name": "brave-search",
  "description": "Brave Search MCP server",
  "command": "npx",
  "args": ["-y", "@modelcontextprotocol/server-brave-search"],
  "env": {"BRAVE_API_KEY": "sk-..."},
  "tags": ["search", "web"],
  "created_at": "2025-01-01T00:00:00Z",
  "updated_at": "2025-01-01T00:00:00Z"
}
```

## Auth Tokens

Tokens are created via `POST /v1/tokens`. The raw token (prefixed `mcp_`) is returned **only once** — only a SHA-256 hash is stored. Pass it to `mcpfleet` CLI via `mcpfleet auth`.

## License

MIT
