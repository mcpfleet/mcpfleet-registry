package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	_ "github.com/mattn/go-sqlite3"

	"github.com/mcpfleet/registry/internal/api"
	"github.com/mcpfleet/registry/internal/db"
	authmw "github.com/mcpfleet/registry/internal/middleware"
)

const usage = `usage:
  registry [serve]                 run the API server (default)
  registry token create --name N   create an auth token and print it

Both read DATABASE_URL (default ./registry.db); serve also reads PORT.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run dispatches the command line and returns the process exit code:
// 0 on success, 1 on a runtime error, 2 on a usage error.
func run(args []string, stdout, stderr io.Writer) int {
	switch {
	case len(args) == 0 || args[0] == "serve":
		if len(args) > 1 {
			fmt.Fprint(stderr, usage)
			return 2
		}
		return serve(stderr)
	case args[0] == "token" && len(args) > 1 && args[1] == "create":
		return tokenCreate(args[2:], stdout, stderr)
	case args[0] == "help" || args[0] == "-h" || args[0] == "--help":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprint(stderr, usage)
		return 2
	}
}

func databaseURL() string {
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		return dsn
	}
	return "./registry.db"
}

// openDB opens the SQLite database at dsn with the options every command
// uses. Callers run db.Migrate themselves.
func openDB(dsn string) (*sql.DB, error) {
	return sql.Open("sqlite3", dsn+"?_journal_mode=WAL&_foreign_keys=on")
}

// openStore opens and migrates the database named by DATABASE_URL.
func openStore() (*sql.DB, *db.Store, error) {
	database, err := openDB(databaseURL())
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open database: %w", err)
	}
	if err := db.Migrate(database); err != nil {
		database.Close()
		return nil, nil, fmt.Errorf("failed to run migrations: %w", err)
	}
	return database, db.NewStore(database), nil
}

// tokenCreate creates a token straight in the database. It is the recovery
// path for an operator who lost every token: it needs access to the host and
// the database file, not the network API. The token alone goes to stdout.
func tokenCreate(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("registry token create", flag.ContinueOnError)
	fs.SetOutput(stderr)
	name := fs.String("name", "", "token name (required)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *name == "" || fs.NArg() > 0 {
		fmt.Fprintln(stderr, "registry token create: --name is required and takes no other arguments")
		fs.Usage()
		return 2
	}

	database, store, err := openStore()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer database.Close()

	result, err := store.CreateToken(context.Background(), *name)
	if err != nil {
		fmt.Fprintf(stderr, "failed to create token: %v\n", err)
		return 1
	}
	fmt.Fprintf(stderr, "Created token %q (id %s). Store it now; it is not shown again.\n", result.Name, result.ID)
	fmt.Fprintln(stdout, result.RawToken)
	return 0
}

func serve(stderr io.Writer) int {
	logger := log.New(stderr, "", log.LstdFlags)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	database, store, err := openStore()
	if err != nil {
		logger.Print(err)
		return 1
	}
	defer database.Close()

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RealIP)

	// Public endpoints — no auth required
	// /docs, /openapi.json served by Huma; /healthz served below
	r.Use(authmw.BearerAuth(store,
		"/docs",
		"/openapi",
		"/healthz",
		"/favicon",
		"/bootstrap",
	))

	// Health check
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	config := huma.DefaultConfig("mcpfleet Registry API", "1.0.0")
	config.Info.Description = "REST API for managing MCP server definitions and auth tokens"
	config.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"bearerAuth": {
			Type:         "http",
			Scheme:       "bearer",
			BearerFormat: "opaque",
		},
	}
	config.Security = []map[string][]string{
		{"bearerAuth": {}},
	}

	humaAPI := humachi.New(r, config)
	api.RegisterRoutes(humaAPI, store)

	addr := fmt.Sprintf(":%s", port)
	logger.Printf("registry listening on %s (auth enabled)", addr)
	if err := http.ListenAndServe(addr, r); err != nil {
		logger.Printf("server error: %v", err)
		return 1
	}
	return 0
}
