# mcpfleet-registry

Self-hosted REST API (Go, Huma v2 + chi, SQLite) that stores MCP server definitions and auth tokens for the `mcpfleet` CLI. The CLI lives in `mcpfleet/mcpfleet`; the website and docs in `mcpfleet/mcpfleet.github.io`.

## Commands

The SQLite driver (`go-sqlite3`) needs cgo: `CGO_ENABLED=1` and a C compiler with the SQLite headers (`gcc`, `libsqlite3-dev`).

```bash
CGO_ENABLED=1 go build ./...          # build every package
CGO_ENABLED=1 go vet ./...            # static checks (CI runs this)
CGO_ENABLED=1 go test ./... -count=1  # unit tests
go run ./cmd/registry                 # run the API locally
docker compose up --build             # build the image and run it on :8080
```

CI (`.github/workflows/ci.yml`) runs `go mod tidy`, build and vet with cgo on Go 1.23 for every PR to `main`.

## Platform

```yaml
tracker: linear
vcs: github
linear:
  team: NER
  project: 056e7cde-601b-4c2b-a981-5015d37a3de4
github:
  owner: mcpfleet
  repo: mcpfleet-registry
```
