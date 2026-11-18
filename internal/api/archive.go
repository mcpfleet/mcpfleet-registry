package api

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	// MaxSkillArchiveBytes is the largest tar.gz archive PUT /v1/skills/{name}
	// accepts; a larger body gets 413.
	MaxSkillArchiveBytes = 1 << 20
	// maxSkillUnpackedBytes caps the total size of the files in an archive,
	// so a small archive cannot unpack into a huge directory.
	maxSkillUnpackedBytes = 10 << 20
	// maxSkillFiles caps the number of entries in an archive.
	maxSkillFiles = 1000
)

// archiveError is a problem with an uploaded skill archive, reported to the
// client as 422.
type archiveError struct {
	msg string
}

func (e *archiveError) Error() string { return e.msg }

func archiveErrorf(format string, args ...any) error {
	return &archiveError{fmt.Sprintf(format, args...)}
}

// unpackSkillArchive extracts the tar.gz archive into dest and returns the
// path of the skill directory, dest/name. The archive must hold exactly one
// top-level directory, name, with only directories and regular files under
// it. Entry paths are relative and stay inside that directory (no absolute
// paths, no "..", no backslashes); a file keeps only its executable bit (0755
// or 0644); a path may appear once. pax global headers are skipped. Every
// violation is an *archiveError.
func unpackSkillArchive(archive []byte, name, dest string) (string, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return "", archiveErrorf("archive is not gzip-compressed: %v", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)

	root := filepath.Join(dest, name)
	var unpacked int64
	entries := 0
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", archiveErrorf("archive is not a valid tar.gz: %v", err)
		}
		if hdr.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		entries++
		if entries > maxSkillFiles {
			return "", archiveErrorf("archive holds more than %d files and directories", maxSkillFiles)
		}

		parts, err := entryPath(hdr.Name)
		if err != nil {
			return "", err
		}
		if len(parts) == 0 {
			continue // the archive root, "./"
		}
		if parts[0] != name {
			return "", archiveErrorf("archive must hold only the directory %q (found %q)", name, hdr.Name)
		}
		target := filepath.Join(append([]string{dest}, parts...)...)

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return "", archiveErrorf("archive entry %q: %v", hdr.Name, err)
			}
		case tar.TypeReg, tar.TypeRegA:
			if len(parts) == 1 {
				return "", archiveErrorf("archive must hold the directory %q, not a file", name)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return "", archiveErrorf("archive entry %q: %v", hdr.Name, err)
			}
			mode := os.FileMode(0o644)
			if hdr.Mode&0o111 != 0 {
				mode = 0o755
			}
			n, err := writeEntry(target, mode, tr, maxSkillUnpackedBytes-unpacked)
			if errors.Is(err, os.ErrExist) {
				return "", archiveErrorf("archive holds %q more than once", hdr.Name)
			}
			if err != nil {
				return "", err
			}
			unpacked += n
		default:
			return "", archiveErrorf("archive entry %q: only regular files and directories are allowed", hdr.Name)
		}
	}

	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return "", archiveErrorf("archive must hold the directory %q", name)
	}
	return root, nil
}

// entryPath splits a tar entry name into its path components, dropping a
// leading "./" and a trailing "/". It rejects absolute paths, backslashes,
// NUL and "." or ".." components.
func entryPath(name string) ([]string, error) {
	bad := archiveErrorf("archive entry %q: path must be relative and stay inside the skill directory", name)
	if strings.HasPrefix(name, "/") || strings.ContainsAny(name, "\\\x00") {
		return nil, bad
	}
	name = strings.TrimSuffix(strings.TrimPrefix(name, "./"), "/")
	if name == "" || name == "." {
		return nil, nil
	}
	parts := strings.Split(name, "/")
	for _, p := range parts {
		if p == "" || p == "." || p == ".." {
			return nil, bad
		}
	}
	return parts, nil
}

// writeEntry creates target, which must not exist, and copies at most limit
// bytes of r into it.
func writeEntry(target string, mode os.FileMode, r io.Reader, limit int64) (int64, error) {
	f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return 0, err
		}
		return 0, archiveErrorf("archive entry %q: %v", filepath.Base(target), err)
	}
	n, err := io.Copy(f, io.LimitReader(r, limit+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return n, archiveErrorf("archive is not a valid tar.gz: %v", err)
	}
	if n > limit {
		return n, archiveErrorf("archive unpacks to more than %d bytes", maxSkillUnpackedBytes)
	}
	// OpenFile applies the umask; set the mode the hash depends on exactly.
	if err := os.Chmod(target, mode); err != nil {
		return n, err
	}
	return n, nil
}
