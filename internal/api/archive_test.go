package api

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tarEntry is one entry of a test archive. A zero typeflag means a regular
// file; mode 0 means 0644.
type tarEntry struct {
	name     string
	body     string
	mode     int64
	typeflag byte
	linkname string
}

func buildArchive(t *testing.T, entries ...tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		hdr := &tar.Header{Name: e.name, Mode: e.mode, Typeflag: e.typeflag, Linkname: e.linkname}
		if hdr.Typeflag == 0 {
			hdr.Typeflag = tar.TypeReg
		}
		if hdr.Mode == 0 {
			hdr.Mode = 0o644
			if hdr.Typeflag == tar.TypeDir {
				hdr.Mode = 0o755
			}
		}
		if hdr.Typeflag == tar.TypeXGlobalHeader {
			// git archive writes one of these with the commit id.
			hdr = &tar.Header{Typeflag: tar.TypeXGlobalHeader, PAXRecords: map[string]string{"comment": "abc"}}
		}
		if hdr.Typeflag == tar.TypeReg {
			hdr.Size = int64(len(e.body))
		}
		require.NoError(t, tw.WriteHeader(hdr))
		if hdr.Typeflag == tar.TypeReg {
			_, err := tw.Write([]byte(e.body))
			require.NoError(t, err)
		}
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	return buf.Bytes()
}

func TestUnpackSkillArchive_Valid(t *testing.T) {
	archive := buildArchive(t,
		tarEntry{name: "./", typeflag: tar.TypeDir},
		tarEntry{name: "./notes/", typeflag: tar.TypeDir},
		tarEntry{name: "./notes/SKILL.md", body: "---\nname: notes\n---\n"},
		tarEntry{name: "notes/scripts/run.sh", body: "#!/bin/sh\n", mode: 0o700},
		tarEntry{name: "pax_global_header", typeflag: tar.TypeXGlobalHeader},
	)
	dest := t.TempDir()

	dir, err := unpackSkillArchive(archive, "notes", dest)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dest, "notes"), dir)

	b, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	require.NoError(t, err)
	assert.Equal(t, "---\nname: notes\n---\n", string(b))

	info, err := os.Stat(filepath.Join(dir, "scripts", "run.sh"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o755), info.Mode().Perm(), "an executable bit makes the file 0755")
	info, err = os.Stat(filepath.Join(dir, "SKILL.md"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o644), info.Mode().Perm())
}

func TestUnpackSkillArchive_Rejects(t *testing.T) {
	skillMD := tarEntry{name: "notes/SKILL.md", body: "x"}
	cases := []struct {
		name    string
		archive func(t *testing.T) []byte
		want    string
	}{
		{"not gzip", func(*testing.T) []byte { return []byte("plain text") }, "gzip"},
		{"empty", func(t *testing.T) []byte { return buildArchive(t) }, "directory \"notes\""},
		{"absolute path", func(t *testing.T) []byte {
			return buildArchive(t, skillMD, tarEntry{name: "/etc/passwd", body: "x"})
		}, "path"},
		{"parent traversal", func(t *testing.T) []byte {
			return buildArchive(t, skillMD, tarEntry{name: "notes/../../escape", body: "x"})
		}, "path"},
		{"backslash", func(t *testing.T) []byte {
			return buildArchive(t, skillMD, tarEntry{name: `notes\..\escape`, body: "x"})
		}, "path"},
		{"other top-level directory", func(t *testing.T) []byte {
			return buildArchive(t, skillMD, tarEntry{name: "other/file", body: "x"})
		}, "only the directory \"notes\""},
		{"top-level file", func(t *testing.T) []byte {
			return buildArchive(t, tarEntry{name: "notes", body: "x"})
		}, "directory \"notes\""},
		{"symlink", func(t *testing.T) []byte {
			return buildArchive(t, skillMD, tarEntry{name: "notes/link", typeflag: tar.TypeSymlink, linkname: "/etc/passwd"})
		}, "regular files and directories"},
		{"hard link", func(t *testing.T) []byte {
			return buildArchive(t, skillMD, tarEntry{name: "notes/link", typeflag: tar.TypeLink, linkname: "notes/SKILL.md"})
		}, "regular files and directories"},
		{"duplicate file", func(t *testing.T) []byte {
			return buildArchive(t, skillMD, skillMD)
		}, "more than once"},
		{"too many files", func(t *testing.T) []byte {
			entries := []tarEntry{skillMD}
			for i := 0; i < maxSkillFiles; i++ {
				entries = append(entries, tarEntry{name: "notes/f" + strconv.Itoa(i)})
			}
			return buildArchive(t, entries...)
		}, "files"},
		{"unpacked size over the limit", func(t *testing.T) []byte {
			return buildArchive(t, tarEntry{name: "notes/big", body: strings.Repeat("0", maxSkillUnpackedBytes+1)})
		}, "unpacks to more than"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := unpackSkillArchive(tc.archive(t), "notes", t.TempDir())
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}
