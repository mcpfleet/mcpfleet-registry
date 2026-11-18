package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"strings"
	"time"
)

// Skill is an Agent Skill stored in the registry: its frontmatter fields,
// the MCP servers it requires and the identity of its archive. The archive
// itself is read with GetSkillArchive.
type Skill struct {
	ID            string
	Name          string
	Description   string
	License       string
	Compatibility string
	AllowedTools  string
	Metadata      map[string]string
	// Requires lists the names of the registry servers the skill needs.
	Requires []string
	// SHA256 is the skillspec hash of the skill directory.
	SHA256 string
	// Size is the size of the tar.gz archive in bytes.
	Size      int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// queryer is the subset of *sql.DB and *sql.Tx the skill helpers need.
type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

const skillColumns = `id, name, description, license, compatibility, allowed_tools, metadata, requires, sha256, size, created_at, updated_at`

func scanSkill(scan func(dest ...any) error) (*Skill, error) {
	var sk Skill
	var metadataJSON, requiresJSON string
	if err := scan(&sk.ID, &sk.Name, &sk.Description, &sk.License, &sk.Compatibility, &sk.AllowedTools,
		&metadataJSON, &requiresJSON, &sk.SHA256, &sk.Size, &sk.CreatedAt, &sk.UpdatedAt); err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(metadataJSON), &sk.Metadata)
	_ = json.Unmarshal([]byte(requiresJSON), &sk.Requires)
	return &sk, nil
}

// ListSkills returns every skill without its archive, ordered by name.
func (s *Store) ListSkills(ctx context.Context) ([]Skill, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+skillColumns+` FROM skills ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var skills []Skill
	for rows.Next() {
		sk, err := scanSkill(rows.Scan)
		if err != nil {
			return nil, err
		}
		skills = append(skills, *sk)
	}
	return skills, rows.Err()
}

// GetSkill returns the skill with the given name, or ErrNotFound.
func (s *Store) GetSkill(ctx context.Context, name string) (*Skill, error) {
	sk, err := scanSkill(s.db.QueryRowContext(ctx, `SELECT `+skillColumns+` FROM skills WHERE name = ?`, name).Scan)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return sk, err
}

// GetSkillArchive returns the stored tar.gz archive of a skill and its
// sha256, or ErrNotFound.
func (s *Store) GetSkillArchive(ctx context.Context, name string) ([]byte, string, error) {
	var archive []byte
	var sha string
	err := s.db.QueryRowContext(ctx, `SELECT archive, sha256 FROM skills WHERE name = ?`, name).Scan(&archive, &sha)
	if err == sql.ErrNoRows {
		return nil, "", ErrNotFound
	}
	if err != nil {
		return nil, "", err
	}
	return archive, sha, nil
}

// UpsertSkill stores sk with its archive, replacing the skill of the same
// name. A replaced skill keeps its id and created_at. Every server in
// sk.Requires must exist: otherwise nothing is written and the error is a
// *MissingServersError (ErrMissingServers). The check and the write run in
// one transaction. created reports whether the skill is new. UpsertSkill
// fills sk's id, size and timestamps.
func (s *Store) UpsertSkill(ctx context.Context, sk *Skill, archive []byte) (created bool, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	missing, err := missingServers(ctx, tx, sk.Requires)
	if err != nil {
		return false, err
	}
	if len(missing) > 0 {
		return false, &MissingServersError{Servers: missing}
	}

	now := time.Now().UTC()
	var id string
	var createdAt time.Time
	err = tx.QueryRowContext(ctx, `SELECT id, created_at FROM skills WHERE name = ?`, sk.Name).Scan(&id, &createdAt)
	switch {
	case err == sql.ErrNoRows:
		created = true
		id = newID()
		createdAt = now
	case err != nil:
		return false, err
	}

	metadataJSON := "{}"
	if len(sk.Metadata) > 0 {
		b, _ := json.Marshal(sk.Metadata)
		metadataJSON = string(b)
	}
	requiresJSON := "[]"
	if len(sk.Requires) > 0 {
		b, _ := json.Marshal(sk.Requires)
		requiresJSON = string(b)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO skills (`+skillColumns+`, archive) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(name) DO UPDATE SET description=excluded.description, license=excluded.license,
			compatibility=excluded.compatibility, allowed_tools=excluded.allowed_tools, metadata=excluded.metadata,
			requires=excluded.requires, sha256=excluded.sha256, size=excluded.size, updated_at=excluded.updated_at,
			archive=excluded.archive`,
		id, sk.Name, sk.Description, sk.License, sk.Compatibility, sk.AllowedTools, metadataJSON, requiresJSON,
		sk.SHA256, int64(len(archive)), createdAt, now, archive)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	sk.ID, sk.Size, sk.CreatedAt, sk.UpdatedAt = id, int64(len(archive)), createdAt, now
	return created, nil
}

// DeleteSkill removes the skill with the given name, or returns ErrNotFound.
func (s *Store) DeleteSkill(ctx context.Context, name string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM skills WHERE name = ?`, name)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// missingServers returns the names in names that no server has, sorted and
// without duplicates.
func missingServers(ctx context.Context, q queryer, names []string) ([]string, error) {
	if len(names) == 0 {
		return nil, nil
	}
	rows, err := q.QueryContext(ctx, `SELECT name FROM servers WHERE name IN (`+placeholders(len(names))+`)`, anys(names)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	found := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		found[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var missing []string
	for _, name := range names {
		if !found[name] {
			found[name] = true // report each name once
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	return missing, nil
}

// skillsRequiring returns the names of the skills whose requires list holds
// any of servers, sorted.
func skillsRequiring(ctx context.Context, q queryer, servers []string) ([]string, error) {
	rows, err := q.QueryContext(ctx, `SELECT DISTINCT skills.name FROM skills, json_each(skills.requires) AS r
		WHERE r.value IN (`+placeholders(len(servers))+`) ORDER BY skills.name`, anys(servers)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func anys(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}
