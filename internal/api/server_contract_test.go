package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests pin the server JSON contract shared with the mcpfleet CLI:
// optional fields may be omitted, env entries are {value?, secret?} objects,
// and /v1/servers/{id} accepts either the server id or its name.

func doJSON(t *testing.T, method, url, body string) (*http.Response, []byte) {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, r)
	require.NoError(t, err)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp, b
}

func TestCreateServer_MinimalBody(t *testing.T) {
	dbConn, server := setupTestAPI(t)
	defer server.Close()
	defer dbConn.Close()

	resp, body := doJSON(t, http.MethodPost, server.URL+"/v1/servers",
		`{"name":"minimal","transport":"stdio","command":"npx"}`)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))

	var out map[string]any
	require.NoError(t, json.Unmarshal(body, &out))
	assert.Equal(t, "minimal", out["name"])
	assert.Equal(t, "npx", out["command"])
	assert.NotEmpty(t, out["id"])
}

func TestCreateServer_EmptyNameIs422(t *testing.T) {
	dbConn, server := setupTestAPI(t)
	defer server.Close()
	defer dbConn.Close()

	for _, payload := range []string{
		`{"name":"","command":"npx"}`,
		`{"command":"npx"}`,
	} {
		resp, body := doJSON(t, http.MethodPost, server.URL+"/v1/servers", payload)
		assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode, "%s -> %s", payload, body)
	}
}

func TestServerEnv_SecretReference(t *testing.T) {
	dbConn, server := setupTestAPI(t)
	defer server.Close()
	defer dbConn.Close()

	resp, body := doJSON(t, http.MethodPost, server.URL+"/v1/servers",
		`{"name":"github","command":"npx","env":{"GITHUB_TOKEN":{"secret":"github_token"},"LOG_LEVEL":{"value":"debug"}}}`)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))

	resp, body = doJSON(t, http.MethodGet, server.URL+"/v1/servers/github", "")
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))

	var out struct {
		Env map[string]map[string]string `json:"env"`
	}
	require.NoError(t, json.Unmarshal(body, &out))
	assert.Equal(t, map[string]map[string]string{
		"GITHUB_TOKEN": {"secret": "github_token"},
		"LOG_LEVEL":    {"value": "debug"},
	}, out.Env)
}

func TestServer_ByName(t *testing.T) {
	dbConn, server := setupTestAPI(t)
	defer server.Close()
	defer dbConn.Close()

	resp, body := doJSON(t, http.MethodPost, server.URL+"/v1/servers",
		`{"name":"by-name","command":"npx"}`)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))

	resp, body = doJSON(t, http.MethodGet, server.URL+"/v1/servers/by-name", "")
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))

	resp, body = doJSON(t, http.MethodPut, server.URL+"/v1/servers/by-name",
		`{"name":"by-name","command":"uvx","description":"updated"}`)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	var out map[string]any
	require.NoError(t, json.Unmarshal(body, &out))
	assert.Equal(t, "uvx", out["command"])
	assert.Equal(t, "stdio", out["transport"], "omitted transport keeps the stdio default")

	resp, body = doJSON(t, http.MethodDelete, server.URL+"/v1/servers/by-name", "")
	require.Equal(t, http.StatusNoContent, resp.StatusCode, string(body))

	resp, _ = doJSON(t, http.MethodGet, server.URL+"/v1/servers/by-name", "")
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// A remote (http or sse) server carries a url instead of a command; the
// registry stores it and returns it on every read.
func TestServer_RemoteURLRoundTrip(t *testing.T) {
	dbConn, server := setupTestAPI(t)
	defer server.Close()
	defer dbConn.Close()

	resp, body := doJSON(t, http.MethodPost, server.URL+"/v1/servers",
		`{"name":"remote","transport":"http","command":"","url":"https://example.com/mcp"}`)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))

	resp, body = doJSON(t, http.MethodGet, server.URL+"/v1/servers/remote", "")
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	var out map[string]any
	require.NoError(t, json.Unmarshal(body, &out))
	assert.Equal(t, "http", out["transport"])
	assert.Equal(t, "https://example.com/mcp", out["url"])

	resp, body = doJSON(t, http.MethodPut, server.URL+"/v1/servers/remote",
		`{"name":"remote","transport":"sse","url":"https://example.com/sse"}`)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))

	resp, body = doJSON(t, http.MethodGet, server.URL+"/v1/servers", "")
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	var list []map[string]any
	require.NoError(t, json.Unmarshal(body, &list))
	require.Len(t, list, 1)
	assert.Equal(t, "sse", list[0]["transport"])
	assert.Equal(t, "https://example.com/sse", list[0]["url"])
}

// Responses always contain every field, so a stdio server reads back with an
// empty url.
func TestServer_StdioHasEmptyURL(t *testing.T) {
	dbConn, server := setupTestAPI(t)
	defer server.Close()
	defer dbConn.Close()

	resp, body := doJSON(t, http.MethodPost, server.URL+"/v1/servers",
		`{"name":"local","transport":"stdio","command":"npx"}`)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	var out map[string]any
	require.NoError(t, json.Unmarshal(body, &out))
	url, ok := out["url"]
	assert.True(t, ok, "url missing from %s", body)
	assert.Equal(t, "", url)
}
