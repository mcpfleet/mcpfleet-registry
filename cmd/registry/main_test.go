package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mcpfleet/registry/internal/db"
)

func TestRun_TokenCreate(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "registry.db")
	t.Setenv("DATABASE_URL", dsn)

	var stdout, stderr bytes.Buffer
	code := run([]string{"token", "create", "--name", "rescue"}, &stdout, &stderr)
	require.Equal(t, 0, code, stderr.String())

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	token := lines[len(lines)-1]
	assert.True(t, strings.HasPrefix(token, "mcp_"), "last stdout line is the token: %q", stdout.String())

	database, err := openDB(dsn)
	require.NoError(t, err)
	defer database.Close()
	store := db.NewStore(database)
	ctx := context.Background()

	valid, err := store.ValidateToken(ctx, token)
	require.NoError(t, err)
	assert.True(t, valid)

	done, err := store.IsBootstrapped(ctx)
	require.NoError(t, err)
	assert.True(t, done, "a token created from the command closes bootstrap")
}

func TestRun_TokenCreateWithoutNameIsUsageError(t *testing.T) {
	t.Setenv("DATABASE_URL", filepath.Join(t.TempDir(), "registry.db"))

	var stdout, stderr bytes.Buffer
	code := run([]string{"token", "create"}, &stdout, &stderr)
	assert.Equal(t, 2, code)
	assert.Empty(t, stdout.String())
	assert.Contains(t, stderr.String(), "--name")
}

func TestRun_UnknownCommandIsUsageError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"nope"}, &stdout, &stderr)
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr.String(), "usage")
}

func TestRun_TokenCreateHelpIsSuccess(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"token", "create", "-h"}, &stdout, &stderr)
	assert.Equal(t, 0, code)
	assert.Contains(t, stderr.String(), "-name")
}
