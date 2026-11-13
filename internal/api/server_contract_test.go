package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
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

// A server name must work as one segment of /v1/servers/{id}: "." and ".."
// are removed by clients and proxies that normalize paths, and control
// characters break terminal output and agent config keys. Create and
// update reject such names with 422 at body.name.
func TestServerName_InvalidIs422(t *testing.T) {
	dbConn, server := setupTestAPI(t)
	defer server.Close()
	defer dbConn.Close()

	resp, body := doJSON(t, http.MethodPost, server.URL+"/v1/servers",
		`{"name":"existing","transport":"stdio","command":"npx"}`)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))

	cases := []struct {
		name    string
		message string
	}{
		{".", `name must not be "." or ".."`},
		{"..", `name must not be "." or ".."`},
		{" ", "name must not be empty or only whitespace"},
		{"a\nb", "name must not contain control characters (found U+000A at byte 1)"},
		{"a\x00b", "name must not contain control characters (found U+0000 at byte 1)"},
		{"a\x7fb", "name must not contain control characters (found U+007F at byte 1)"},
		{"a\u0085b", "name must not contain control characters (found U+0085 at byte 1)"},
		{"a\u202eb", "name must not contain invisible format characters (found U+202E at byte 1)"},
		{"\ufeffgithub", "name must not contain invisible format characters (found U+FEFF at byte 0)"},
		{"zażółć\u200b", "name must not contain invisible format characters (found U+200B at byte 10)"},
	}
	for _, tc := range cases {
		for _, method := range []string{http.MethodPost, http.MethodPut} {
			t.Run(method+" "+strconv.Quote(tc.name), func(t *testing.T) {
				url := server.URL + "/v1/servers"
				if method == http.MethodPut {
					url += "/existing"
				}
				payload, err := json.Marshal(map[string]any{"name": tc.name, "command": "npx"})
				require.NoError(t, err)
				resp, body := doJSON(t, method, url, string(payload))
				require.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode, string(body))

				var p struct {
					Detail string `json:"detail"`
					Errors []struct {
						Location string `json:"location"`
						Message  string `json:"message"`
					} `json:"errors"`
				}
				require.NoError(t, json.Unmarshal(body, &p), string(body))
				assert.Equal(t, "validation failed", p.Detail)
				require.Len(t, p.Errors, 1, string(body))
				assert.Equal(t, "body.name", p.Errors[0].Location)
				assert.Equal(t, tc.message, p.Errors[0].Message)
			})
		}
	}
}

// Names that only look close to the rejected ones stay valid.
func TestServerName_ValidEdgeCasesAccepted(t *testing.T) {
	dbConn, server := setupTestAPI(t)
	defer server.Close()
	defer dbConn.Close()

	for _, name := range []string{".a", "a..b", "...", "a/b", "zażółć", " a "} {
		t.Run(name, func(t *testing.T) {
			payload, err := json.Marshal(map[string]any{"name": name, "command": "npx"})
			require.NoError(t, err)
			resp, body := doJSON(t, http.MethodPost, server.URL+"/v1/servers", string(payload))
			require.Equal(t, http.StatusOK, resp.StatusCode, string(body))

			resp, body = doJSON(t, http.MethodPut, server.URL+"/v1/servers/"+url.PathEscape(name), string(payload))
			require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
		})
	}
}

// A remote server carries HTTP headers whose values are literals or secret
// references; the registry stores and returns them like env. Every response
// has a headers object, empty for a server without headers.
func TestServer_HeadersRoundTrip(t *testing.T) {
	dbConn, server := setupTestAPI(t)
	defer server.Close()
	defer dbConn.Close()

	resp, body := doJSON(t, http.MethodPost, server.URL+"/v1/servers",
		`{"name":"remote","transport":"http","url":"https://example.com/mcp","headers":{"Authorization":{"secret":"example_auth"},"X-Client":{"value":"mcpfleet"}}}`)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))

	type headersOut struct {
		Headers map[string]map[string]string `json:"headers"`
	}
	want := map[string]map[string]string{
		"Authorization": {"secret": "example_auth"},
		"X-Client":      {"value": "mcpfleet"},
	}
	resp, body = doJSON(t, http.MethodGet, server.URL+"/v1/servers/remote", "")
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	var got headersOut
	require.NoError(t, json.Unmarshal(body, &got))
	assert.Equal(t, want, got.Headers)

	resp, body = doJSON(t, http.MethodPut, server.URL+"/v1/servers/remote",
		`{"name":"remote","transport":"sse","url":"https://example.com/sse","headers":{"X-Api-Key":{"secret":"example_key"}}}`)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))

	resp, body = doJSON(t, http.MethodGet, server.URL+"/v1/servers", "")
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	var list []headersOut
	require.NoError(t, json.Unmarshal(body, &list))
	require.Len(t, list, 1)
	assert.Equal(t, map[string]map[string]string{"X-Api-Key": {"secret": "example_key"}}, list[0].Headers)

	resp, body = doJSON(t, http.MethodPost, server.URL+"/v1/servers",
		`{"name":"local","command":"npx"}`)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	var raw map[string]any
	require.NoError(t, json.Unmarshal(body, &raw))
	headers, ok := raw["headers"]
	assert.True(t, ok, "headers missing from %s", body)
	assert.Equal(t, map[string]any{}, headers)
}

// Headers are only for remote servers; names must be HTTP tokens without
// case-insensitive duplicates, each entry holds exactly one of value and
// secret, values cannot break the header line, and credential headers must
// reference a secret, since the registry never stores secret values.
func TestServer_HeadersValidation(t *testing.T) {
	dbConn, server := setupTestAPI(t)
	defer server.Close()
	defer dbConn.Close()

	resp, body := doJSON(t, http.MethodPost, server.URL+"/v1/servers",
		`{"name":"existing","transport":"http","url":"https://example.com/mcp"}`)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))

	const remote = `"name":"a","transport":"http","url":"https://example.com/mcp"`
	type detail struct{ location, message string }
	cases := []struct {
		name string
		body string
		want []detail
	}{
		{"stdio with headers", `{"name":"a","command":"npx","headers":{"X-Client":{"value":"x"}}}`,
			[]detail{{"body.headers", "headers is only used for transport http or sse"}}},
		{"name with space", `{` + remote + `,"headers":{"X Client":{"value":"x"}}}`,
			[]detail{{"body.headers.X Client", "header name must be an HTTP token"}}},
		{"name with colon", `{` + remote + `,"headers":{"X-Client:":{"value":"x"}}}`,
			[]detail{{"body.headers.X-Client:", "header name must be an HTTP token"}}},
		{"empty name", `{` + remote + `,"headers":{"":{"value":"x"}}}`,
			[]detail{{"body.headers.", "header name must be an HTTP token"}}},
		{"duplicate name", `{` + remote + `,"headers":{"X-Client":{"value":"a"},"x-client":{"value":"b"}}}`,
			[]detail{{"body.headers.x-client", "duplicate header (case-insensitive): x-client"}}},
		{"value and secret", `{` + remote + `,"headers":{"X-Client":{"value":"a","secret":"b"}}}`,
			[]detail{{"body.headers.X-Client", "header needs exactly one of value or secret"}}},
		{"neither value nor secret", `{` + remote + `,"headers":{"X-Client":{}}}`,
			[]detail{{"body.headers.X-Client", "header needs exactly one of value or secret"}}},
		{"value with CR LF", `{` + remote + `,"headers":{"X-Client":{"value":"a\r\nX-Evil: 1"}}}`,
			[]detail{{"body.headers.X-Client", "header value must not contain CR, LF or NUL"}}},
		{"value with NUL", `{` + remote + `,"headers":{"X-Client":{"value":"a\u0000b"}}}`,
			[]detail{{"body.headers.X-Client", "header value must not contain CR, LF or NUL"}}},
		{"literal Authorization", `{` + remote + `,"headers":{"authorization":{"value":"Bearer t0ken"}}}`,
			[]detail{{"body.headers.authorization", `use {"secret": ...} for credential headers; the registry does not store secret values`}}},
		{"literal Proxy-Authorization", `{` + remote + `,"headers":{"Proxy-Authorization":{"value":"Basic x"}}}`,
			[]detail{{"body.headers.Proxy-Authorization", `use {"secret": ...} for credential headers; the registry does not store secret values`}}},
		{"literal Cookie", `{` + remote + `,"headers":{"Cookie":{"value":"s=1"}}}`,
			[]detail{{"body.headers.Cookie", `use {"secret": ...} for credential headers; the registry does not store secret values`}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, method := range []string{http.MethodPost, http.MethodPut} {
				url := server.URL + "/v1/servers"
				if method == http.MethodPut {
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
				assert.NotContains(t, string(body), "t0ken", "a rejected header value is not echoed")
			}
		})
	}
}

// Header names that are valid tokens, a secret Authorization and literal
// non-credential headers are accepted.
func TestServer_HeadersValidAccepted(t *testing.T) {
	dbConn, server := setupTestAPI(t)
	defer server.Close()
	defer dbConn.Close()

	resp, body := doJSON(t, http.MethodPost, server.URL+"/v1/servers",
		`{"name":"a","transport":"sse","url":"https://example.com/sse","headers":{"Authorization":{"secret":"a_auth"},"Cookie":{"secret":"a_cookie"},"X-Odd!#$%&'*+.^_`+"`"+`|~1":{"value":"v a l\tue"}}}`)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
}
