package db

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL&_foreign_keys=on")
	require.NoError(t, err)
	require.NoError(t, Migrate(db))
	return db
}

func TestStoreServers(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := NewStore(db)
	ctx := context.Background()

	t.Run("CreateServer", func(t *testing.T) {
		srv := &Server{
			Name:        "test-server",
			Description: "Test MCP server",
			Transport:   "stdio",
			Install:     map[string]string{"type": "npx", "package": "@test/server"},
			Command:     "npx",
			Args:        []string{"-y", "@test/server"},
			Env:         map[string]string{"TEST_VAR": "test_value"},
			Tags:        []string{"dev", "test"},
			Platforms:   []string{"linux", "darwin"},
		}

		err := store.CreateServer(ctx, srv)
		require.NoError(t, err)
		assert.NotEmpty(t, srv.ID)
		assert.False(t, srv.CreatedAt.IsZero())
		assert.False(t, srv.UpdatedAt.IsZero())
	})

	t.Run("GetServer", func(t *testing.T) {
		srv := &Server{
			Name:        "get-test",
			Description: "Test get server",
			Transport:   "stdio",
			Install:     map[string]string{"type": "npx"},
			Command:     "npx",
		}
		require.NoError(t, store.CreateServer(ctx, srv))

		fetched, err := store.GetServer(ctx, srv.ID)
		require.NoError(t, err)
		require.NotNil(t, fetched)
		assert.Equal(t, srv.Name, fetched.Name)
		assert.Equal(t, srv.Description, fetched.Description)
		assert.Equal(t, srv.Command, fetched.Command)
	})

	t.Run("ListServers", func(t *testing.T) {
		// Create multiple servers
		for i := 0; i < 3; i++ {
			srv := &Server{
				Name:      fmt.Sprintf("list-test-%d", i),
				Transport: "stdio",
				Install:   map[string]string{"type": "npx"},
				Command:   "npx",
			}
			require.NoError(t, store.CreateServer(ctx, srv))
		}

		servers, err := store.ListServers(ctx)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(servers), 3)
	})

	t.Run("UpdateServer", func(t *testing.T) {
		srv := &Server{
			Name:        "update-test",
			Description: "Original description",
			Transport:   "stdio",
			Install:     map[string]string{"type": "npx"},
			Command:     "npx",
		}
		require.NoError(t, store.CreateServer(ctx, srv))

		// Update
		srv.Description = "Updated description"
		srv.Args = []string{"-y", "updated"}
		err := store.UpdateServer(ctx, srv)
		require.NoError(t, err)

		// Verify
		fetched, err := store.GetServer(ctx, srv.ID)
		require.NoError(t, err)
		assert.Equal(t, "Updated description", fetched.Description)
		assert.Equal(t, []string{"-y", "updated"}, fetched.Args)
	})

	t.Run("DeleteServer", func(t *testing.T) {
		srv := &Server{
			Name:      "delete-test",
			Transport: "stdio",
			Install:   map[string]string{"type": "npx"},
			Command:   "npx",
		}
		require.NoError(t, store.CreateServer(ctx, srv))

		err := store.DeleteServer(ctx, srv.ID)
		require.NoError(t, err)

		// Verify deleted
		fetched, err := store.GetServer(ctx, srv.ID)
		require.NoError(t, err)
		assert.Nil(t, fetched)
	})
}

func TestStoreTokens(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := NewStore(db)
	ctx := context.Background()

	t.Run("CreateToken", func(t *testing.T) {
		result, err := store.CreateToken(ctx, "test-token")
		require.NoError(t, err)
		assert.NotEmpty(t, result.ID)
		assert.Equal(t, "test-token", result.Name)
		assert.NotEmpty(t, result.RawToken)
		assert.Contains(t, result.RawToken, "mcp_")
		assert.False(t, result.CreatedAt.IsZero())
	})

	t.Run("ListTokens", func(t *testing.T) {
		// Create multiple tokens
		for i := 0; i < 3; i++ {
			_, err := store.CreateToken(ctx, fmt.Sprintf("token-%d", i))
			require.NoError(t, err)
		}

		tokens, err := store.ListTokens(ctx)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(tokens), 3)
	})

	t.Run("DeleteToken", func(t *testing.T) {
		result, err := store.CreateToken(ctx, "delete-token")
		require.NoError(t, err)

		err = store.DeleteToken(ctx, result.ID)
		require.NoError(t, err)

		// Verify deleted
		tokens, err := store.ListTokens(ctx)
		require.NoError(t, err)
		for _, token := range tokens {
			assert.NotEqual(t, result.ID, token.ID)
		}
	})
}

func TestValidateToken(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := NewStore(db)
	ctx := context.Background()

	t.Run("ValidToken", func(t *testing.T) {
		result, err := store.CreateToken(ctx, "valid-token")
		require.NoError(t, err)

		valid, err := store.ValidateToken(ctx, result.RawToken)
		require.NoError(t, err)
		assert.True(t, valid)
	})

	t.Run("InvalidToken", func(t *testing.T) {
		valid, err := store.ValidateToken(ctx, "mcp_invalidtoken")
		require.NoError(t, err)
		assert.False(t, valid)
	})

	t.Run("TokenUpdatesLastUsedAt", func(t *testing.T) {
		result, err := store.CreateToken(ctx, "lastused-token")
		require.NoError(t, err)

		// First validation
		valid, err := store.ValidateToken(ctx, result.RawToken)
		require.NoError(t, err)
		assert.True(t, valid)

		// Check LastUsedAt is set
		tokens, err := store.ListTokens(ctx)
		require.NoError(t, err)
		var token *Token
		for _, t := range tokens {
			if t.ID == result.ID {
				token = &t
				break
			}
		}
		require.NotNil(t, token)
		assert.NotNil(t, token.LastUsedAt)
	})
}
