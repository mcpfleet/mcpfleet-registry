package main

import (
	"database/sql"
	"fmt"
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

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "./registry.db"
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	database, err := sql.Open("sqlite3", dsn+"?_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer database.Close()

	if err := db.Migrate(database); err != nil {
		log.Fatalf("failed to run migrations: %v", err)
	}

	store := db.NewStore(database)

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
	log.Printf("registry listening on %s (auth enabled)", addr)
	if err := http.ListenAndServe(addr, r); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
