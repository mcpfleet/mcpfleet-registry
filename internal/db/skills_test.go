package db

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testSkill(name string, requires ...string) *Skill {
	return &Skill{
		Name:          name,
		Description:   "Does " + name + " things.",
		License:       "MIT",
		Compatibility: "Requires git",
		AllowedTools:  "Bash(git:*) Read",
		Metadata:      map[string]string{"author": "example"},
		Requires:      requires,
		SHA256:        "abc123",
	}
}

// Migration 11 creates the skills table on a database that stopped at
// version 10, and the servers it already holds survive.
func TestMigrate_AddsSkillsTable(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	// Roll the database back to how version 10 left it.
	_, err := db.Exec(`DROP TABLE skills; DELETE FROM schema_version WHERE version >= 11`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO servers (id, name, command) VALUES ('s1', 'fs', 'npx')`)
	require.NoError(t, err)

	require.NoError(t, Migrate(db))

	store := NewStore(db)
	ctx := context.Background()
	_, err = store.UpsertSkill(ctx, testSkill("git-helper", "fs"), []byte("archive"))
	require.NoError(t, err)
	srv, err := store.GetServer(ctx, "fs")
	require.NoError(t, err)
	require.NotNil(t, srv)
}

func TestStore_SkillRoundTrip(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	store := NewStore(db)
	ctx := context.Background()
	require.NoError(t, store.CreateServer(ctx, &Server{Name: "github", Transport: "stdio", Command: "gh"}))

	created, err := store.UpsertSkill(ctx, testSkill("git-helper", "github"), []byte("tar.gz bytes"))
	require.NoError(t, err)
	assert.True(t, created)

	got, err := store.GetSkill(ctx, "git-helper")
	require.NoError(t, err)
	assert.NotEmpty(t, got.ID)
	assert.Equal(t, "git-helper", got.Name)
	assert.Equal(t, "Does git-helper things.", got.Description)
	assert.Equal(t, "MIT", got.License)
	assert.Equal(t, "Requires git", got.Compatibility)
	assert.Equal(t, "Bash(git:*) Read", got.AllowedTools)
	assert.Equal(t, map[string]string{"author": "example"}, got.Metadata)
	assert.Equal(t, []string{"github"}, got.Requires)
	assert.Equal(t, "abc123", got.SHA256)
	assert.Equal(t, int64(len("tar.gz bytes")), got.Size)
	assert.False(t, got.CreatedAt.IsZero())

	archive, sha, err := store.GetSkillArchive(ctx, "git-helper")
	require.NoError(t, err)
	assert.Equal(t, []byte("tar.gz bytes"), archive)
	assert.Equal(t, "abc123", sha)

	list, err := store.ListSkills(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "git-helper", list[0].Name)
	assert.Equal(t, []string{"github"}, list[0].Requires)
}

func TestStore_UpsertSkillReplaces(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	store := NewStore(db)
	ctx := context.Background()

	_, err := store.UpsertSkill(ctx, testSkill("notes"), []byte("v1"))
	require.NoError(t, err)
	first, err := store.GetSkill(ctx, "notes")
	require.NoError(t, err)

	second := testSkill("notes")
	second.Description = "Second version."
	second.SHA256 = "def456"
	second.Metadata = nil
	created, err := store.UpsertSkill(ctx, second, []byte("version 2"))
	require.NoError(t, err)
	assert.False(t, created)

	got, err := store.GetSkill(ctx, "notes")
	require.NoError(t, err)
	assert.Equal(t, first.ID, got.ID)
	assert.Equal(t, first.CreatedAt.Unix(), got.CreatedAt.Unix())
	assert.Equal(t, "Second version.", got.Description)
	assert.Equal(t, "def456", got.SHA256)
	assert.Equal(t, int64(len("version 2")), got.Size)
	assert.Empty(t, got.Metadata)
	assert.Empty(t, got.Requires)

	list, err := store.ListSkills(ctx)
	require.NoError(t, err)
	assert.Len(t, list, 1)
}

func TestStore_UpsertSkillMissingServers(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	store := NewStore(db)
	ctx := context.Background()
	require.NoError(t, store.CreateServer(ctx, &Server{Name: "github", Transport: "stdio", Command: "gh"}))

	_, err := store.UpsertSkill(ctx, testSkill("deploy", "github", "slack", "aws"), []byte("x"))
	assert.ErrorIs(t, err, ErrMissingServers)
	var missing *MissingServersError
	require.ErrorAs(t, err, &missing)
	assert.Equal(t, []string{"aws", "slack"}, missing.Servers)

	_, err = store.GetSkill(ctx, "deploy")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestStore_SkillNotFound(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	store := NewStore(db)
	ctx := context.Background()

	_, err := store.GetSkill(ctx, "nope")
	assert.ErrorIs(t, err, ErrNotFound)
	_, _, err = store.GetSkillArchive(ctx, "nope")
	assert.ErrorIs(t, err, ErrNotFound)
	assert.ErrorIs(t, store.DeleteSkill(ctx, "nope"), ErrNotFound)
}

func TestStore_DeleteSkill(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	store := NewStore(db)
	ctx := context.Background()

	_, err := store.UpsertSkill(ctx, testSkill("notes"), []byte("x"))
	require.NoError(t, err)
	require.NoError(t, store.DeleteSkill(ctx, "notes"))
	_, err = store.GetSkill(ctx, "notes")
	assert.ErrorIs(t, err, ErrNotFound)
}

// A server that a skill requires cannot be deleted, by name or by id; the
// error names every dependent skill. Once the skills are gone it can.
func TestStore_DeleteServerRequiredBySkill(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	store := NewStore(db)
	ctx := context.Background()
	gh := &Server{Name: "github", Transport: "stdio", Command: "gh"}
	require.NoError(t, store.CreateServer(ctx, gh))
	require.NoError(t, store.CreateServer(ctx, &Server{Name: "slack", Transport: "stdio", Command: "slack"}))
	_, err := store.UpsertSkill(ctx, testSkill("review", "github"), []byte("x"))
	require.NoError(t, err)
	_, err = store.UpsertSkill(ctx, testSkill("deploy", "slack", "github"), []byte("x"))
	require.NoError(t, err)
	_, err = store.UpsertSkill(ctx, testSkill("chat", "slack"), []byte("x"))
	require.NoError(t, err)

	for _, ref := range []string{"github", gh.ID} {
		err = store.DeleteServer(ctx, ref)
		assert.ErrorIs(t, err, ErrServerRequired)
		var required *ServerRequiredError
		require.ErrorAs(t, err, &required)
		assert.Equal(t, []string{"deploy", "review"}, required.Skills)
	}
	srv, err := store.GetServer(ctx, "github")
	require.NoError(t, err)
	require.NotNil(t, srv)

	require.NoError(t, store.DeleteSkill(ctx, "review"))
	require.NoError(t, store.DeleteSkill(ctx, "deploy"))
	assert.NoError(t, store.DeleteServer(ctx, "github"))
}
