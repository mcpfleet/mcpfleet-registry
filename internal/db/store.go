package db

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

type Server struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Transport   string            `json:"transport"` // stdio | sse | http
	Install     map[string]string `json:"install"`   // e.g. {"type":"npx","package":"..."}
	Command     string            `json:"command"`
	Args        []string          `json:"args"`
	Env         map[string]EnvVar `json:"env"`
	Tags        []string          `json:"tags"`
	Platforms   []string          `json:"platforms"` // linux, darwin, windows
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// EnvVar is either a literal value or a reference to a secret stored in the
// client-side vault. The registry stores the reference, never the secret.
type EnvVar struct {
	Value  string `json:"value,omitempty"`
	Secret string `json:"secret,omitempty"`
}

// UnmarshalJSON also accepts the legacy string form, so rows written before
// env became an object read back as {"value": "<string>"}.
func (e *EnvVar) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*e = EnvVar{Value: s}
		return nil
	}
	type plain EnvVar
	return json.Unmarshal(b, (*plain)(e))
}

type Token struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// Servers

func (s *Store) ListServers(ctx context.Context) ([]Server, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, description, transport, install, command, args, env, tags, platforms, created_at, updated_at FROM servers ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var servers []Server
	for rows.Next() {
		var srv Server
		var installJSON, argsJSON, envJSON, tagsJSON, platformsJSON string
		if err := rows.Scan(&srv.ID, &srv.Name, &srv.Description, &srv.Transport, &installJSON, &srv.Command, &argsJSON, &envJSON, &tagsJSON, &platformsJSON, &srv.CreatedAt, &srv.UpdatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(installJSON), &srv.Install)
		_ = json.Unmarshal([]byte(argsJSON), &srv.Args)
		_ = json.Unmarshal([]byte(envJSON), &srv.Env)
		_ = json.Unmarshal([]byte(tagsJSON), &srv.Tags)
		_ = json.Unmarshal([]byte(platformsJSON), &srv.Platforms)
		servers = append(servers, srv)
	}
	return servers, rows.Err()
}

// GetServer looks a server up by its id or by its unique name.
func (s *Store) GetServer(ctx context.Context, id string) (*Server, error) {
	var srv Server
	var installJSON, argsJSON, envJSON, tagsJSON, platformsJSON string
	err := s.db.QueryRowContext(ctx, `SELECT id, name, description, transport, install, command, args, env, tags, platforms, created_at, updated_at FROM servers WHERE id = ? OR name = ?`, id, id).Scan(
		&srv.ID, &srv.Name, &srv.Description, &srv.Transport, &installJSON, &srv.Command, &argsJSON, &envJSON, &tagsJSON, &platformsJSON, &srv.CreatedAt, &srv.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(installJSON), &srv.Install)
	_ = json.Unmarshal([]byte(argsJSON), &srv.Args)
	_ = json.Unmarshal([]byte(envJSON), &srv.Env)
	_ = json.Unmarshal([]byte(tagsJSON), &srv.Tags)
	_ = json.Unmarshal([]byte(platformsJSON), &srv.Platforms)
	return &srv, nil
}

func (s *Store) CreateServer(ctx context.Context, srv *Server) error {
	srv.ID = newID()
	srv.CreatedAt = time.Now().UTC()
	srv.UpdatedAt = srv.CreatedAt
	installJSON, _ := json.Marshal(srv.Install)
	argsJSON, _ := json.Marshal(srv.Args)
	envJSON, _ := json.Marshal(srv.Env)
	tagsJSON, _ := json.Marshal(srv.Tags)
	platformsJSON, _ := json.Marshal(srv.Platforms)
	_, err := s.db.ExecContext(ctx, `INSERT INTO servers (id, name, description, transport, install, command, args, env, tags, platforms, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		srv.ID, srv.Name, srv.Description, srv.Transport, string(installJSON), srv.Command, string(argsJSON), string(envJSON), string(tagsJSON), string(platformsJSON), srv.CreatedAt, srv.UpdatedAt)
	return err
}

func (s *Store) UpdateServer(ctx context.Context, srv *Server) error {
	srv.UpdatedAt = time.Now().UTC()
	installJSON, _ := json.Marshal(srv.Install)
	argsJSON, _ := json.Marshal(srv.Args)
	envJSON, _ := json.Marshal(srv.Env)
	tagsJSON, _ := json.Marshal(srv.Tags)
	platformsJSON, _ := json.Marshal(srv.Platforms)
	_, err := s.db.ExecContext(ctx, `UPDATE servers SET name=?, description=?, transport=?, install=?, command=?, args=?, env=?, tags=?, platforms=?, updated_at=? WHERE id=?`,
		srv.Name, srv.Description, srv.Transport, string(installJSON), srv.Command, string(argsJSON), string(envJSON), string(tagsJSON), string(platformsJSON), srv.UpdatedAt, srv.ID)
	return err
}

// DeleteServer removes the server whose id or unique name matches.
func (s *Store) DeleteServer(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM servers WHERE id = ? OR name = ?`, id, id)
	return err
}

// Tokens

func (s *Store) ListTokens(ctx context.Context) ([]Token, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, created_at, last_used_at FROM tokens ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tokens []Token
	for rows.Next() {
		var t Token
		if err := rows.Scan(&t.ID, &t.Name, &t.CreatedAt, &t.LastUsedAt); err != nil {
			return nil, err
		}
		tokens = append(tokens, t)
	}
	return tokens, rows.Err()
}

type CreateTokenResult struct {
	Token
	RawToken string `json:"token"`
}

func (s *Store) CreateToken(ctx context.Context, name string) (*CreateTokenResult, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, fmt.Errorf("generate token: %w", err)
	}
	rawHex := "mcp_" + hex.EncodeToString(raw)
	hash := sha256.Sum256([]byte(rawHex))
	hashHex := hex.EncodeToString(hash[:])
	t := &CreateTokenResult{}
	t.ID = newID()
	t.Name = name
	t.CreatedAt = time.Now().UTC()
	t.RawToken = rawHex
	_, err := s.db.ExecContext(ctx, `INSERT INTO tokens (id, name, hash, created_at) VALUES (?,?,?,?)`,
		t.ID, t.Name, hashHex, t.CreatedAt)
	if err != nil {
		return nil, err
	}
	return t, nil
}

func (s *Store) DeleteToken(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM tokens WHERE id = ?`, id)
	return err
}

func (s *Store) ValidateToken(ctx context.Context, raw string) (bool, error) {
	hash := sha256.Sum256([]byte(raw))
	hashHex := hex.EncodeToString(hash[:])
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM tokens WHERE hash = ?`, hashHex).Scan(&id)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE tokens SET last_used_at = ? WHERE id = ?`, time.Now().UTC(), id)
	return true, nil
}

func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
