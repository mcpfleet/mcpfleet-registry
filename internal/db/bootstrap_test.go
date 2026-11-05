package db

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStore_BootstrapOnce(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	store := NewStore(db)
	ctx := context.Background()

	done, err := store.IsBootstrapped(ctx)
	require.NoError(t, err)
	assert.False(t, done, "a fresh database is open for bootstrap")

	first, err := store.Bootstrap(ctx, "admin")
	require.NoError(t, err)
	assert.Equal(t, "admin", first.Name)

	done, err = store.IsBootstrapped(ctx)
	require.NoError(t, err)
	assert.True(t, done)

	_, err = db.Exec(`DELETE FROM tokens`)
	require.NoError(t, err)

	_, err = store.Bootstrap(ctx, "again")
	assert.ErrorIs(t, err, ErrAlreadyBootstrapped)

	tokens, err := store.ListTokens(ctx)
	require.NoError(t, err)
	assert.Empty(t, tokens, "a rejected bootstrap must not leave a token behind")
}

func TestStore_CreateTokenMarksBootstrapped(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	store := NewStore(db)
	ctx := context.Background()

	_, err := store.CreateToken(ctx, "cli")
	require.NoError(t, err)

	done, err := store.IsBootstrapped(ctx)
	require.NoError(t, err)
	assert.True(t, done)

	_, err = store.Bootstrap(ctx, "admin")
	assert.ErrorIs(t, err, ErrAlreadyBootstrapped)
}
