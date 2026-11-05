package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests pin the bootstrap lifecycle: POST /bootstrap works once in the
// life of a database, and the API cannot remove the last token.

func bootstrapToken(t *testing.T, url string) (id, token string) {
	t.Helper()
	resp, body := doJSON(t, http.MethodPost, url+"/bootstrap", `{"name":"admin"}`)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	var out struct {
		ID    string `json:"id"`
		Token string `json:"token"`
	}
	require.NoError(t, json.Unmarshal(body, &out))
	return out.ID, out.Token
}

// Removing every token must not reopen bootstrap: an empty token table on a
// used registry is not a fresh install.
func TestBootstrap_ClosedAfterAllTokensRemoved(t *testing.T) {
	dbConn, server := setupTestAPI(t)
	defer server.Close()
	defer dbConn.Close()

	bootstrapToken(t, server.URL)
	_, err := dbConn.Exec(`DELETE FROM tokens`)
	require.NoError(t, err)

	resp, body := doJSON(t, http.MethodPost, server.URL+"/bootstrap", `{"name":"attacker"}`)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode, string(body))
}
