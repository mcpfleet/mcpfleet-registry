package db

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStore_DeleteUnknownServerIsNotFound(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	store := NewStore(db)

	assert.ErrorIs(t, store.DeleteServer(context.Background(), "nope"), ErrNotFound)
}

func TestStore_DuplicateServerNameIsTaken(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	store := NewStore(db)
	ctx := context.Background()

	first := &Server{Name: "first", Transport: "stdio"}
	require.NoError(t, store.CreateServer(ctx, first))
	second := &Server{Name: "second", Transport: "stdio"}
	require.NoError(t, store.CreateServer(ctx, second))

	assert.ErrorIs(t, store.CreateServer(ctx, &Server{Name: "first", Transport: "stdio"}), ErrNameTaken)

	second.Name = "first"
	assert.ErrorIs(t, store.UpdateServer(ctx, second), ErrNameTaken)

	first.Description = "same name keeps working"
	assert.NoError(t, store.UpdateServer(ctx, first))
}
