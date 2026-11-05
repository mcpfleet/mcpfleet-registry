package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mcpfleet/registry/internal/db"
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

// Deleting the only token would lock everyone out; the API refuses with 409,
// the token keeps working and bootstrap stays closed.
func TestDeleteToken_LastIs409(t *testing.T) {
	dbConn, server := setupTestAPI(t)
	defer server.Close()
	defer dbConn.Close()

	id, token := bootstrapToken(t, server.URL)

	resp, body := doJSON(t, http.MethodDelete, server.URL+"/v1/tokens/"+id, "")
	assert.Equal(t, http.StatusConflict, resp.StatusCode, string(body))
	assert.Equal(t, "cannot delete the last token", problemDetail(t, body))

	valid, err := db.NewStore(dbConn).ValidateToken(context.Background(), token)
	require.NoError(t, err)
	assert.True(t, valid, "the last token must still be valid")

	resp, body = doJSON(t, http.MethodPost, server.URL+"/bootstrap", `{"name":"attacker"}`)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode, string(body))
}

// With two tokens one can be deleted, and bootstrap stays closed.
func TestDeleteToken_NotLastIs204(t *testing.T) {
	dbConn, server := setupTestAPI(t)
	defer server.Close()
	defer dbConn.Close()

	id, _ := bootstrapToken(t, server.URL)
	_, err := db.NewStore(dbConn).CreateToken(context.Background(), "second")
	require.NoError(t, err)

	resp, body := doJSON(t, http.MethodDelete, server.URL+"/v1/tokens/"+id, "")
	assert.Equal(t, http.StatusNoContent, resp.StatusCode, string(body))

	resp, body = doJSON(t, http.MethodPost, server.URL+"/bootstrap", `{"name":"attacker"}`)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode, string(body))
}

func TestDeleteToken_UnknownIs404(t *testing.T) {
	dbConn, server := setupTestAPI(t)
	defer server.Close()
	defer dbConn.Close()

	bootstrapToken(t, server.URL)

	resp, body := doJSON(t, http.MethodDelete, server.URL+"/v1/tokens/nope", "")
	assert.Equal(t, http.StatusNotFound, resp.StatusCode, string(body))
	assert.Equal(t, "token not found", problemDetail(t, body))
}
