package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/mcpfleet/mcpfleet/pkg/skillspec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mcpfleet/registry/internal/db"
	authmw "github.com/mcpfleet/registry/internal/middleware"
)

const notesSkillMD = `---
name: notes
description: Take notes in a markdown file.
license: MIT
compatibility: Requires git
allowed-tools: Bash(git:*) Read
metadata:
  author: example
  mcpfleet-requires: github
---
# Notes

Write the notes to NOTES.md.
`

// skillFiles is the content of the notes test skill, relative to the skill
// directory.
var skillFiles = []tarEntry{
	{name: "SKILL.md", body: notesSkillMD},
	{name: "scripts/run.sh", body: "#!/bin/sh\necho hi\n", mode: 0o755},
}

// skillArchive packs files under the top-level directory dir.
func skillArchive(t *testing.T, dir string, files ...tarEntry) []byte {
	t.Helper()
	entries := []tarEntry{{name: dir + "/", typeflag: '5'}}
	for _, f := range files {
		f.name = dir + "/" + f.name
		entries = append(entries, f)
	}
	return buildArchive(t, entries...)
}

// skillHash writes files to a directory named dir and returns their
// skillspec hash, the value the registry must compute for the same skill.
func skillHash(t *testing.T, dir string, files ...tarEntry) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), dir)
	for _, f := range files {
		p := filepath.Join(root, filepath.FromSlash(f.name))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		mode := os.FileMode(0o644)
		if f.mode != 0 {
			mode = os.FileMode(f.mode)
		}
		require.NoError(t, os.WriteFile(p, []byte(f.body), mode))
		require.NoError(t, os.Chmod(p, mode))
	}
	h, err := skillspec.Hash(root)
	require.NoError(t, err)
	return h
}

func putSkill(t *testing.T, base, name string, archive []byte) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, base+"/v1/skills/"+name, bytes.NewReader(archive))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/gzip")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp, body
}

func createGitHubServer(t *testing.T, store *db.Store) {
	t.Helper()
	require.NoError(t, store.CreateServer(context.Background(), &db.Server{Name: "github", Transport: "stdio", Command: "gh"}))
}

type problem struct {
	Status int    `json:"status"`
	Detail string `json:"detail"`
	Errors []struct {
		Location string `json:"location"`
		Message  string `json:"message"`
		Value    any    `json:"value"`
	} `json:"errors"`
	Skills []string `json:"skills"`
}

func decodeProblem(t *testing.T, body []byte) problem {
	t.Helper()
	var p problem
	require.NoError(t, json.Unmarshal(body, &p), string(body))
	return p
}

func errorLocations(p problem) []string {
	var locs []string
	for _, e := range p.Errors {
		locs = append(locs, e.Location)
	}
	return locs
}

func TestPutSkill_Valid(t *testing.T) {
	dbConn, server := setupTestAPI(t)
	defer server.Close()
	defer dbConn.Close()
	createGitHubServer(t, db.NewStore(dbConn))

	archive := skillArchive(t, "notes", skillFiles...)
	resp, body := putSkill(t, server.URL, "notes", archive)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))

	var out map[string]any
	require.NoError(t, json.Unmarshal(body, &out))
	assert.Equal(t, "notes", out["name"])
	assert.Equal(t, "Take notes in a markdown file.", out["description"])
	assert.Equal(t, "MIT", out["license"])
	assert.Equal(t, "Requires git", out["compatibility"])
	assert.Equal(t, "Bash(git:*) Read", out["allowed_tools"])
	assert.Equal(t, map[string]any{"author": "example", "mcpfleet-requires": "github"}, out["metadata"])
	assert.Equal(t, []any{"github"}, out["requires"])
	assert.Equal(t, skillHash(t, "notes", skillFiles...), out["sha256"])
	assert.Equal(t, float64(len(archive)), out["size"])
	assert.Equal(t, []any{}, out["warnings"])
}

// The sha256 is the server's own hash of the unpacked content: a lost
// executable bit changes it, and a different archive of the same content
// does not.
func TestPutSkill_SHA256Recomputed(t *testing.T) {
	dbConn, server := setupTestAPI(t)
	defer server.Close()
	defer dbConn.Close()
	createGitHubServer(t, db.NewStore(dbConn))

	_, body := putSkill(t, server.URL, "notes", skillArchive(t, "notes", skillFiles...))
	var first map[string]any
	require.NoError(t, json.Unmarshal(body, &first))

	// Same content, different order and an extra directory entry.
	reordered := skillArchive(t, "notes", tarEntry{name: "scripts/", typeflag: '5'}, skillFiles[1], skillFiles[0])
	_, body = putSkill(t, server.URL, "notes", reordered)
	var second map[string]any
	require.NoError(t, json.Unmarshal(body, &second))
	assert.Equal(t, first["sha256"], second["sha256"])

	notExecutable := []tarEntry{skillFiles[0], {name: "scripts/run.sh", body: skillFiles[1].body, mode: 0o644}}
	_, body = putSkill(t, server.URL, "notes", skillArchive(t, "notes", notExecutable...))
	var third map[string]any
	require.NoError(t, json.Unmarshal(body, &third))
	assert.NotEqual(t, first["sha256"], third["sha256"])
	assert.Equal(t, skillHash(t, "notes", notExecutable...), third["sha256"])
}

func TestPutSkill_SpecViolationsAre422(t *testing.T) {
	_, server := setupTestAPI(t)
	defer server.Close()

	cases := []struct {
		name      string
		path      string
		skillMD   string
		locations []string
	}{
		{"missing SKILL.md", "notes", "", []string{"SKILL.md"}},
		{"name differs from the path", "notes",
			"---\nname: other\ndescription: d\n---\n", []string{"name"}},
		{"uppercase name and no description", "Notes",
			"---\nname: Notes\n---\n", []string{"name", "description"}},
		{"unknown field", "notes",
			"---\nname: notes\ndescription: d\nversion: 1\n---\n", []string{"version"}},
		{"description too long", "notes",
			"---\nname: notes\ndescription: " + strings.Repeat("d", skillspec.MaxDescriptionLength+1) + "\n---\n", []string{"description"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files := []tarEntry{{name: "README.md", body: "x"}}
			if tc.skillMD != "" {
				files = []tarEntry{{name: "SKILL.md", body: tc.skillMD}}
			}
			resp, body := putSkill(t, server.URL, tc.path, skillArchive(t, tc.path, files...))
			require.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode, string(body))
			assert.ElementsMatch(t, tc.locations, errorLocations(decodeProblem(t, body)))
		})
	}

	resp, err := http.Get(server.URL + "/v1/skills")
	require.NoError(t, err)
	defer resp.Body.Close()
	var list []any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&list))
	assert.Empty(t, list, "a rejected skill is not stored")
}

// What skillspec reports as a warning (here: a metadata value that is not a
// string) does not block the upload; the response lists it.
func TestPutSkill_WarningsAccepted(t *testing.T) {
	_, server := setupTestAPI(t)
	defer server.Close()

	skillMD := "---\nname: notes\ndescription: d\nmetadata:\n  tags:\n    - a\n---\n"
	resp, body := putSkill(t, server.URL, "notes", skillArchive(t, "notes", tarEntry{name: "SKILL.md", body: skillMD}))
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	var out struct {
		Warnings []struct {
			Field string `json:"field"`
			Rule  string `json:"rule"`
		} `json:"warnings"`
	}
	require.NoError(t, json.Unmarshal(body, &out))
	require.Len(t, out.Warnings, 1)
	assert.Equal(t, "metadata.tags", out.Warnings[0].Field)
	assert.Equal(t, skillspec.RuleStringValues, out.Warnings[0].Rule)
}

func TestPutSkill_MissingServersAre422(t *testing.T) {
	_, server := setupTestAPI(t)
	defer server.Close()

	skillMD := "---\nname: notes\ndescription: d\nmetadata:\n  mcpfleet-requires: github slack\n---\n"
	resp, body := putSkill(t, server.URL, "notes", skillArchive(t, "notes", tarEntry{name: "SKILL.md", body: skillMD}))
	require.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode, string(body))
	p := decodeProblem(t, body)
	require.Len(t, p.Errors, 2)
	for i, name := range []string{"github", "slack"} {
		assert.Equal(t, "metadata.mcpfleet-requires", p.Errors[i].Location)
		assert.Equal(t, name, p.Errors[i].Value)
	}
}

func TestPutSkill_ArchiveSizeLimit(t *testing.T) {
	_, server := setupTestAPI(t)
	defer server.Close()

	resp, body := putSkill(t, server.URL, "notes", bytes.Repeat([]byte{0}, MaxSkillArchiveBytes+1))
	assert.Equal(t, http.StatusRequestEntityTooLarge, resp.StatusCode, string(body))

	// Exactly at the limit the body is read and judged as an archive.
	resp, body = putSkill(t, server.URL, "notes", bytes.Repeat([]byte{0}, MaxSkillArchiveBytes))
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode, string(body))
}

func TestPutSkill_BadArchiveIs422(t *testing.T) {
	_, server := setupTestAPI(t)
	defer server.Close()

	cases := map[string][]byte{
		"not gzip":  []byte("not an archive"),
		"traversal": buildArchive(t, tarEntry{name: "notes/../../etc/x", body: "x"}),
		"symlink":   buildArchive(t, tarEntry{name: "notes/link", typeflag: '2', linkname: "/etc/passwd"}),
		"wrong dir": skillArchive(t, "other", skillFiles...),
	}
	for name, archive := range cases {
		t.Run(name, func(t *testing.T) {
			resp, body := putSkill(t, server.URL, "notes", archive)
			assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode, string(body))
		})
	}
}

func TestPutSkill_UnusableNameIs422(t *testing.T) {
	_, server := setupTestAPI(t)
	defer server.Close()

	for _, name := range []string{"a%5Cb", "a%2Fb", "%2E%2E"} {
		resp, body := putSkill(t, server.URL, name, skillArchive(t, "notes", skillFiles...))
		assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode, name+": "+string(body))
	}
}

func TestSkills_GetListArchiveDelete(t *testing.T) {
	dbConn, server := setupTestAPI(t)
	defer server.Close()
	defer dbConn.Close()
	createGitHubServer(t, db.NewStore(dbConn))

	archive := skillArchive(t, "notes", skillFiles...)
	resp, body := putSkill(t, server.URL, "notes", archive)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	var put map[string]any
	require.NoError(t, json.Unmarshal(body, &put))

	resp, body = doJSON(t, http.MethodGet, server.URL+"/v1/skills", "")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var list []map[string]any
	require.NoError(t, json.Unmarshal(body, &list))
	require.Len(t, list, 1)
	assert.Equal(t, "notes", list[0]["name"])
	assert.Equal(t, put["sha256"], list[0]["sha256"])

	resp, body = doJSON(t, http.MethodGet, server.URL+"/v1/skills/notes", "")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var one map[string]any
	require.NoError(t, json.Unmarshal(body, &one))
	assert.Equal(t, "Take notes in a markdown file.", one["description"])
	assert.Equal(t, put["sha256"], one["sha256"])
	assert.Equal(t, []any{"github"}, one["requires"])

	resp, body = doJSON(t, http.MethodGet, server.URL+"/v1/skills/notes/archive", "")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, archive, body)
	assert.Equal(t, "application/gzip", resp.Header.Get("Content-Type"))
	assert.Equal(t, put["sha256"], resp.Header.Get("X-Skill-Sha256"))

	resp, _ = doJSON(t, http.MethodDelete, server.URL+"/v1/skills/notes", "")
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)

	for _, path := range []string{"/v1/skills/notes", "/v1/skills/notes/archive"} {
		resp, _ = doJSON(t, http.MethodGet, server.URL+path, "")
		assert.Equal(t, http.StatusNotFound, resp.StatusCode, path)
	}
	resp, _ = doJSON(t, http.MethodDelete, server.URL+"/v1/skills/notes", "")
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// The skill routes sit behind the bearer auth middleware the way cmd/registry
// mounts it.
func TestSkills_RequireAuth(t *testing.T) {
	dbConn, server := setupTestAPI(t)
	defer server.Close()
	defer dbConn.Close()
	store := db.NewStore(dbConn)

	r := chi.NewRouter()
	r.Use(authmw.BearerAuth(store, "/docs", "/openapi", "/healthz", "/favicon", "/bootstrap"))
	RegisterRoutes(humachi.New(r, huma.DefaultConfig("Test API", "1.0.0")), store)
	authed := httptest.NewServer(r)
	defer authed.Close()

	tok, err := store.CreateToken(context.Background(), "test")
	require.NoError(t, err)

	archive := skillArchive(t, "notes", tarEntry{name: "SKILL.md", body: "---\nname: notes\ndescription: d\n---\n"})
	requests := []struct{ method, path string }{
		{http.MethodGet, "/v1/skills"},
		{http.MethodPut, "/v1/skills/notes"},
		{http.MethodGet, "/v1/skills/notes"},
		{http.MethodGet, "/v1/skills/notes/archive"},
		{http.MethodDelete, "/v1/skills/notes"},
	}
	for _, rq := range requests {
		for _, token := range []string{"", tok.RawToken} {
			var body io.Reader
			if rq.method == http.MethodPut {
				body = bytes.NewReader(archive)
			}
			req, err := http.NewRequest(rq.method, authed.URL+rq.path, body)
			require.NoError(t, err)
			if token != "" {
				req.Header.Set("Authorization", "Bearer "+token)
			}
			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			resp.Body.Close()
			if token == "" {
				assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, rq.method+" "+rq.path)
			} else {
				assert.Less(t, resp.StatusCode, 300, rq.method+" "+rq.path)
			}
		}
	}
}

func TestDeleteServerRequiredBySkill(t *testing.T) {
	dbConn, server := setupTestAPI(t)
	defer server.Close()
	defer dbConn.Close()
	createGitHubServer(t, db.NewStore(dbConn))

	resp, body := putSkill(t, server.URL, "notes", skillArchive(t, "notes", skillFiles...))
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))

	resp, body = doJSON(t, http.MethodDelete, server.URL+"/v1/servers/github", "")
	require.Equal(t, http.StatusConflict, resp.StatusCode, string(body))
	p := decodeProblem(t, body)
	assert.Equal(t, []string{"notes"}, p.Skills)
	assert.Contains(t, p.Detail, "notes")

	resp, _ = doJSON(t, http.MethodGet, server.URL+"/v1/servers/github", "")
	assert.Equal(t, http.StatusOK, resp.StatusCode, "the server is still there")

	resp, _ = doJSON(t, http.MethodDelete, server.URL+"/v1/skills/notes", "")
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	resp, _ = doJSON(t, http.MethodDelete, server.URL+"/v1/servers/github", "")
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
}

func TestOpenAPISkills(t *testing.T) {
	_, server := setupTestAPI(t)
	defer server.Close()

	resp, body := doJSON(t, http.MethodGet, server.URL+"/openapi.json", "")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var doc struct {
		Paths map[string]map[string]any `json:"paths"`
	}
	require.NoError(t, json.Unmarshal(body, &doc))
	assert.Contains(t, doc.Paths["/v1/skills"], "get")
	assert.Contains(t, doc.Paths["/v1/skills/{name}"], "put")
	assert.Contains(t, doc.Paths["/v1/skills/{name}"], "get")
	assert.Contains(t, doc.Paths["/v1/skills/{name}"], "delete")
	assert.Contains(t, doc.Paths["/v1/skills/{name}/archive"], "get")
}
