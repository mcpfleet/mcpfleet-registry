package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mcpfleet/registry/internal/db"
)

func setupTestAPI(t *testing.T) (*sql.DB, *httptest.Server) {
	// Create in-memory database
	dbConn, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL&_foreign_keys=on")
	require.NoError(t, err)
	require.NoError(t, db.Migrate(dbConn))

	store := db.NewStore(dbConn)

	// Setup chi router
	r := chi.NewRouter()
	config := huma.DefaultConfig("Test API", "1.0.0")
	humaAPI := humachi.New(r, config)
	RegisterRoutes(humaAPI, store)

	server := httptest.NewServer(r)
	return dbConn, server
}

func TestListServers(t *testing.T) {
	dbConn, server := setupTestAPI(t)
	defer server.Close()
	defer dbConn.Close()

	store := db.NewStore(dbConn)
	ctx := context.Background()

	// Create test server
	srv := &db.Server{
		Name:      "test-server",
		Transport: "stdio",
		Install:   map[string]string{"type": "npx"},
		Command:   "npx",
	}
	require.NoError(t, store.CreateServer(ctx, srv))

	// Test list endpoint
	resp, err := http.Get(server.URL + "/v1/servers")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))

	// Parse JSON response
	var result []map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(result), 1)
}

func TestCreateServer(t *testing.T) {
	_, server := setupTestAPI(t)
	defer server.Close()

	payload := map[string]interface{}{
		"name":        "new-server",
		"description": "",
		"transport":   "stdio",
		"install":     map[string]string{"type": "npx"},
		"command":     "npx",
		"args":        []string{},
		"env":         map[string]string{},
		"tags":        []string{},
		"platforms":   []string{},
	}

	body, _ := json.Marshal(payload)
	resp, err := http.Post(server.URL+"/v1/servers", "application/json", strings.NewReader(string(body)))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	assert.Equal(t, "new-server", result["name"])
	assert.NotEmpty(t, result["id"])
}

func TestGetServer(t *testing.T) {
	dbConn, server := setupTestAPI(t)
	defer server.Close()
	defer dbConn.Close()

	store := db.NewStore(dbConn)
	ctx := context.Background()

	srv := &db.Server{
		Name:      "get-test",
		Transport: "stdio",
		Install:   map[string]string{"type": "npx"},
		Command:   "npx",
	}
	require.NoError(t, store.CreateServer(ctx, srv))

	resp, err := http.Get(server.URL + "/v1/servers/" + srv.ID)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	assert.Equal(t, "get-test", result["name"])
	assert.Equal(t, srv.ID, result["id"])
}

func TestUpdateServer(t *testing.T) {
	dbConn, server := setupTestAPI(t)
	defer server.Close()
	defer dbConn.Close()

	store := db.NewStore(dbConn)
	ctx := context.Background()

	srv := &db.Server{
		Name:        "update-test",
		Description: "Original",
		Transport:   "stdio",
		Install:     map[string]string{"type": "npx"},
		Command:     "npx",
	}
	require.NoError(t, store.CreateServer(ctx, srv))

	payload := map[string]interface{}{
		"name":        "update-test",
		"description": "Updated",
		"transport":   "stdio",
		"install":     map[string]string{"type": "npx"},
		"command":     "npx",
		"args":        []string{},
		"env":         map[string]string{},
		"tags":        []string{},
		"platforms":   []string{},
	}

	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest("PUT", server.URL+"/v1/servers/"+srv.ID, strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestDeleteServer(t *testing.T) {
	dbConn, server := setupTestAPI(t)
	defer server.Close()
	defer dbConn.Close()

	store := db.NewStore(dbConn)
	ctx := context.Background()

	srv := &db.Server{
		Name:      "delete-test",
		Transport: "stdio",
		Install:   map[string]string{"type": "npx"},
		Command:   "npx",
	}
	require.NoError(t, store.CreateServer(ctx, srv))

	req, _ := http.NewRequest("DELETE", server.URL+"/v1/servers/"+srv.ID, nil)
	client := &http.Client{}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNoContent, resp.StatusCode)

	// Verify deleted
	fetched, err := store.GetServer(ctx, srv.ID)
	require.NoError(t, err)
	assert.Nil(t, fetched)
}

func TestListTokens(t *testing.T) {
	dbConn, server := setupTestAPI(t)
	defer server.Close()
	defer dbConn.Close()

	store := db.NewStore(dbConn)
	ctx := context.Background()

	// Create test tokens
	for i := 0; i < 3; i++ {
		_, err := store.CreateToken(ctx, "token-"+string(rune('A'+i)))
		require.NoError(t, err)
	}

	resp, err := http.Get(server.URL + "/v1/tokens")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result []map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(result), 3)
}

func TestCreateToken(t *testing.T) {
	_, server := setupTestAPI(t)
	defer server.Close()

	payload := map[string]interface{}{
		"name": "test-token",
	}

	body, _ := json.Marshal(payload)
	resp, err := http.Post(server.URL+"/v1/tokens", "application/json", strings.NewReader(string(body)))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	assert.Equal(t, "test-token", result["name"])
	assert.NotEmpty(t, result["token"])
	assert.Contains(t, result["token"], "mcp_")
}

func TestBootstrap(t *testing.T) {
	dbConn, server := setupTestAPI(t)
	defer server.Close()
	defer dbConn.Close()

	store := db.NewStore(dbConn)
	ctx := context.Background()

	t.Run("First bootstrap succeeds", func(t *testing.T) {
		payload := map[string]interface{}{
			"name": "admin",
		}

		body, _ := json.Marshal(payload)
		resp, err := http.Post(server.URL+"/bootstrap", "application/json", strings.NewReader(string(body)))
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var result map[string]interface{}
		err = json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)

		assert.Equal(t, "admin", result["name"])
		assert.NotEmpty(t, result["token"])
		assert.Contains(t, result["token"], "mcp_")

		// Verify token was created in database
		tokens, err := store.ListTokens(ctx)
		require.NoError(t, err)
		assert.Len(t, tokens, 1)
		assert.Equal(t, "admin", tokens[0].Name)
	})

	t.Run("Second bootstrap fails", func(t *testing.T) {
		// Seed a token so this subtest does not depend on the first one.
		tokens, err := store.ListTokens(ctx)
		require.NoError(t, err)
		if len(tokens) == 0 {
			_, err = store.CreateToken(ctx, "admin")
			require.NoError(t, err)
		}

		payload := map[string]interface{}{
			"name": "admin2",
		}

		body, _ := json.Marshal(payload)
		resp, err := http.Post(server.URL+"/bootstrap", "application/json", strings.NewReader(string(body)))
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	})
}

func TestDeleteToken(t *testing.T) {
	dbConn, server := setupTestAPI(t)
	defer server.Close()
	defer dbConn.Close()

	store := db.NewStore(dbConn)
	ctx := context.Background()

	result, err := store.CreateToken(ctx, "delete-test")
	require.NoError(t, err)

	req, _ := http.NewRequest("DELETE", server.URL+"/v1/tokens/"+result.ID, nil)
	client := &http.Client{}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNoContent, resp.StatusCode)

	// Verify deleted
	tokens, err := store.ListTokens(ctx)
	require.NoError(t, err)
	assert.Len(t, tokens, 0)
}

// postJSON sends payload as a flat JSON request body, the shape huma expects.
func postJSON(t *testing.T, url string, payload interface{}) *http.Response {
	t.Helper()
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	resp, err := http.Post(url, "application/json", strings.NewReader(string(body)))
	require.NoError(t, err)
	return resp
}

// A valid bootstrap request must be rejected with 403 once any token exists.
func TestBootstrap_RejectsWhenTokenExists(t *testing.T) {
	dbConn, server := setupTestAPI(t)
	defer server.Close()
	defer dbConn.Close()

	_, err := db.NewStore(dbConn).CreateToken(context.Background(), "admin")
	require.NoError(t, err)

	resp := postJSON(t, server.URL+"/bootstrap", map[string]interface{}{"name": "admin2"})
	defer resp.Body.Close()

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

// huma validates the request body before the handler runs: a body wrapped in
// a "body" key does not match the schema and is rejected with 422.
func TestBootstrap_WrappedBodyIs422(t *testing.T) {
	dbConn, server := setupTestAPI(t)
	defer server.Close()
	defer dbConn.Close()

	resp := postJSON(t, server.URL+"/bootstrap", map[string]interface{}{
		"body": map[string]interface{}{"name": "x"},
	})
	defer resp.Body.Close()

	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)

	tokens, err := db.NewStore(dbConn).ListTokens(context.Background())
	require.NoError(t, err)
	assert.Empty(t, tokens)
}
