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

// A database migrated before registry_state existed has no flag. Migration 8
// sets it when the registry was used (tokens or servers), so an old database
// whose tokens were all deleted does not stay open for bootstrap.
func TestMigrate_BackfillsBootstrapped(t *testing.T) {
	cases := []struct {
		name string
		seed string
		want bool
	}{
		{"with token", `INSERT INTO tokens (id, name, hash) VALUES ('t1', 'admin', 'h1')`, true},
		{"with server only", `INSERT INTO servers (id, name, command) VALUES ('s1', 'fs', 'npx')`, true},
		{"empty", ``, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := setupTestDB(t)
			defer db.Close()

			// Roll the database back to how version 7 left it.
			_, err := db.Exec(`DELETE FROM registry_state; DELETE FROM schema_version WHERE version >= 8`)
			require.NoError(t, err)
			if tc.seed != "" {
				_, err = db.Exec(tc.seed)
				require.NoError(t, err)
			}

			require.NoError(t, Migrate(db))

			done, err := NewStore(db).IsBootstrapped(context.Background())
			require.NoError(t, err)
			assert.Equal(t, tc.want, done)
		})
	}
}

func TestStore_DeleteToken(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	store := NewStore(db)
	ctx := context.Background()

	assert.ErrorIs(t, store.DeleteToken(ctx, "nope"), ErrNotFound)

	first, err := store.CreateToken(ctx, "first")
	require.NoError(t, err)
	assert.ErrorIs(t, store.DeleteToken(ctx, first.ID), ErrLastToken)

	_, err = store.CreateToken(ctx, "second")
	require.NoError(t, err)
	require.NoError(t, store.DeleteToken(ctx, first.ID))
	assert.ErrorIs(t, store.DeleteToken(ctx, first.ID), ErrNotFound)
}
