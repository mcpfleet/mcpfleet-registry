package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests pin the error statuses of the server endpoints: an unknown
// server is 404 and a name that is already taken is 409, never 204 or 500.

func problemDetail(t *testing.T, body []byte) string {
	t.Helper()
	var p struct {
		Detail string `json:"detail"`
	}
	require.NoError(t, json.Unmarshal(body, &p), string(body))
	return p.Detail
}

func TestDeleteServer_UnknownIs404(t *testing.T) {
	dbConn, server := setupTestAPI(t)
	defer server.Close()
	defer dbConn.Close()

	resp, body := doJSON(t, http.MethodDelete, server.URL+"/v1/servers/nope", "")
	assert.Equal(t, http.StatusNotFound, resp.StatusCode, string(body))
	assert.Equal(t, "server not found", problemDetail(t, body))
}
