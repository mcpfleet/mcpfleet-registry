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

// The transport decides which of command and url a server needs; an
// inconsistent body is 422 with one errors[] entry per problem.
func TestCreateServer_TransportValidation(t *testing.T) {
	dbConn, server := setupTestAPI(t)
	defer server.Close()
	defer dbConn.Close()

	type detail struct{ location, message string }
	cases := []struct {
		name string
		body string
		want []detail
	}{
		{"http without url", `{"name":"a","transport":"http","command":""}`,
			[]detail{{"body.url", "url is required for transport http"}}},
		{"sse without url", `{"name":"a","transport":"sse"}`,
			[]detail{{"body.url", "url is required for transport sse"}}},
		{"stdio without command", `{"name":"a","transport":"stdio"}`,
			[]detail{{"body.command", "command is required for transport stdio"}}},
		{"default transport without command", `{"name":"a","transport":""}`,
			[]detail{{"body.command", "command is required for transport stdio"}}},
		{"stdio with url", `{"name":"a","transport":"stdio","command":"npx","url":"https://example.com/mcp"}`,
			[]detail{{"body.url", "url is only used for transport http or sse"}}},
		{"http with command", `{"name":"a","transport":"http","command":"npx","url":"https://example.com/mcp"}`,
			[]detail{{"body.command", "command is not used for transport http; remove it"}}},
		{"relative url", `{"name":"a","transport":"http","url":"example.com/mcp"}`,
			[]detail{{"body.url", "url must be an absolute http or https URL"}}},
		{"other scheme", `{"name":"a","transport":"sse","url":"ftp://example.com/mcp"}`,
			[]detail{{"body.url", "url must be an absolute http or https URL"}}},
		{"unknown transport", `{"name":"a","transport":"websocket","command":"npx"}`,
			[]detail{{"body.transport", "transport must be stdio, sse or http"}}},
		{"every problem at once", `{"name":"a","transport":"stdio","url":"https://example.com/mcp"}`,
			[]detail{
				{"body.command", "command is required for transport stdio"},
				{"body.url", "url is only used for transport http or sse"},
			}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, method := range []string{http.MethodPost, http.MethodPut} {
				url := server.URL + "/v1/servers"
				if method == http.MethodPut {
					resp, body := doJSON(t, http.MethodPost, url,
						`{"name":"existing","transport":"stdio","command":"npx"}`)
					if resp.StatusCode != http.StatusConflict {
						require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
					}
					url += "/existing"
				}
				resp, body := doJSON(t, method, url, tc.body)
				require.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode, "%s: %s", method, body)

				var p struct {
					Detail string `json:"detail"`
					Errors []struct {
						Location string `json:"location"`
						Message  string `json:"message"`
					} `json:"errors"`
				}
				require.NoError(t, json.Unmarshal(body, &p), string(body))
				assert.Equal(t, "validation failed", p.Detail)
				var got []detail
				for _, e := range p.Errors {
					got = append(got, detail{e.Location, e.Message})
				}
				assert.ElementsMatch(t, tc.want, got, "%s: %s", method, body)
			}
		})
	}
}
