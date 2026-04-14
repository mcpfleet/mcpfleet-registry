package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/mcpfleet/registry/internal/db"
)

type Handler struct {
	store *db.Store
}

func RegisterRoutes(api huma.API, store *db.Store) {
	h := &Handler{store: store}

	// Servers
	huma.Register(api, huma.Operation{
		OperationID: "list-servers",
		Method:      http.MethodGet,
		Path:        "/v1/servers",
		Summary:     "List MCP servers",
		Tags:        []string{"servers"},
	}, h.ListServers)

	huma.Register(api, huma.Operation{
		OperationID: "get-server",
		Method:      http.MethodGet,
		Path:        "/v1/servers/{id}",
		Summary:     "Get MCP server by name",
		Tags:        []string{"servers"},
	}, h.GetServer)

	huma.Register(api, huma.Operation{
		OperationID: "create-server",
		Method:      http.MethodPost,
		Path:        "/v1/servers",
		Summary:     "Create MCP server",
		Tags:        []string{"servers"},
	}, h.CreateServer)

	huma.Register(api, huma.Operation{
		OperationID: "update-server",
		Method:      http.MethodPut,
		Path:        "/v1/servers/{id}",
		Summary:     "Update MCP server",
		Tags:        []string{"servers"},
	}, h.UpdateServer)

	huma.Register(api, huma.Operation{
		OperationID: "delete-server",
		Method:      http.MethodDelete,
		Path:        "/v1/servers/{id}",
		Summary:     "Delete MCP server",
		Tags:        []string{"servers"},
	}, h.DeleteServer)

	// Tokens
	huma.Register(api, huma.Operation{
		OperationID: "list-tokens",
		Method:      http.MethodGet,
		Path:        "/v1/tokens",
		Summary:     "List auth tokens",
		Tags:        []string{"tokens"},
	}, h.ListTokens)

	huma.Register(api, huma.Operation{
		OperationID: "create-token",
		Method:      http.MethodPost,
		Path:        "/v1/tokens",
		Summary:     "Create auth token",
		Tags:        []string{"tokens"},
	}, h.CreateToken)

	huma.Register(api, huma.Operation{
		OperationID: "delete-token",
		Method:      http.MethodDelete,
		Path:        "/v1/tokens/{id}",
		Summary:     "Delete auth token",
		Tags:        []string{"tokens"},
	}, h.DeleteToken)

	// Bootstrap - create first admin token without auth
	huma.Register(api, huma.Operation{
		OperationID: "bootstrap",
		Method:      http.MethodPost,
		Path:        "/bootstrap",
		Summary:     "Create first admin token (no auth required)",
		Tags:        []string{"bootstrap"},
	}, h.Bootstrap)
}

// --- DTOs ---

type ServerOutput struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Transport   string            `json:"transport"`
	Install     map[string]string `json:"install"`
	Command     string            `json:"command"`
	Args        []string          `json:"args"`
	Env         map[string]string `json:"env"`
	Tags        []string          `json:"tags"`
	Platforms   []string          `json:"platforms"`
	CreatedAt   string            `json:"created_at"`
	UpdatedAt   string            `json:"updated_at"`
}

type ServersOutput struct {
	Body []ServerOutput
}

type ServerInput struct {
	Body struct {
		Name        string            `json:"name" required:"true"`
		Description string            `json:"description"`
		Transport   string            `json:"transport"`
		Install     map[string]string `json:"install"`
		Command     string            `json:"command"`
		Args        []string          `json:"args"`
		Env         map[string]string `json:"env"`
		Tags        []string          `json:"tags"`
		Platforms   []string          `json:"platforms"`
	}
}

type IDInput struct {
	ID string `path:"id"`
}

type TokenOutput struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
}

type TokensOutput struct {
	Body []TokenOutput
}

type CreateTokenInput struct {
	Body struct {
		Name string `json:"name" required:"true"`
	}
}

type BootstrapInput struct {
	Body struct {
		Name string `json:"name" required:"true"`
	}
}

type BootstrapOutput struct {
	Body struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Token     string `json:"token"`
		CreatedAt string `json:"created_at"`
	}
}

// --- Helpers ---

func toServerOutput(s db.Server) ServerOutput {
	install := s.Install
	if install == nil {
		install = map[string]string{}
	}
	args := s.Args
	if args == nil {
		args = []string{}
	}
	env := s.Env
	if env == nil {
		env = map[string]string{}
	}
	tags := s.Tags
	if tags == nil {
		tags = []string{}
	}
	platforms := s.Platforms
	if platforms == nil {
		platforms = []string{}
	}
	return ServerOutput{
		ID:          s.ID,
		Name:        s.Name,
		Description: s.Description,
		Transport:   s.Transport,
		Install:     install,
		Command:     s.Command,
		Args:        args,
		Env:         env,
		Tags:        tags,
		Platforms:   platforms,
		CreatedAt:   s.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt:   s.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	}
}

// --- Handlers ---

func (h *Handler) ListServers(ctx context.Context, _ *struct{}) (*ServersOutput, error) {
	servers, err := h.store.ListServers(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("list servers", err)
	}
	out := &ServersOutput{}
	for _, s := range servers {
		out.Body = append(out.Body, toServerOutput(s))
	}
	if out.Body == nil {
		out.Body = []ServerOutput{}
	}
	return out, nil
}

type SingleServerOutput struct {
	Body ServerOutput
}

func (h *Handler) GetServer(ctx context.Context, input *IDInput) (*SingleServerOutput, error) {
	srv, err := h.store.GetServer(ctx, input.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("get server", err)
	}
	if srv == nil {
		return nil, huma.Error404NotFound("server not found")
	}
	return &SingleServerOutput{Body: toServerOutput(*srv)}, nil
}

func (h *Handler) CreateServer(ctx context.Context, input *ServerInput) (*SingleServerOutput, error) {
	srv := &db.Server{
		Name:        input.Body.Name,
		Description: input.Body.Description,
		Transport:   input.Body.Transport,
		Install:     input.Body.Install,
		Command:     input.Body.Command,
		Args:        input.Body.Args,
		Env:         input.Body.Env,
		Tags:        input.Body.Tags,
		Platforms:   input.Body.Platforms,
	}
	if srv.Transport == "" {
		srv.Transport = "stdio"
	}
	if err := h.store.CreateServer(ctx, srv); err != nil {
		return nil, huma.Error500InternalServerError("create server", err)
	}
	return &SingleServerOutput{Body: toServerOutput(*srv)}, nil
}

func (h *Handler) UpdateServer(ctx context.Context, input *struct {
	ID   string `path:"id"`
	Body struct {
		Name        string            `json:"name"`
		Description string            `json:"description"`
		Transport   string            `json:"transport"`
		Install     map[string]string `json:"install"`
		Command     string            `json:"command"`
		Args        []string          `json:"args"`
		Env         map[string]string `json:"env"`
		Tags        []string          `json:"tags"`
		Platforms   []string          `json:"platforms"`
	}
}) (*SingleServerOutput, error) {
	existing, err := h.store.GetServer(ctx, input.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("get server", err)
	}
	if existing == nil {
		return nil, huma.Error404NotFound("server not found")
	}
	existing.Name = input.Body.Name
	existing.Description = input.Body.Description
	existing.Transport = input.Body.Transport
	existing.Install = input.Body.Install
	existing.Command = input.Body.Command
	existing.Args = input.Body.Args
	existing.Env = input.Body.Env
	existing.Tags = input.Body.Tags
	existing.Platforms = input.Body.Platforms
	if err := h.store.UpdateServer(ctx, existing); err != nil {
		return nil, huma.Error500InternalServerError("update server", err)
	}
	return &SingleServerOutput{Body: toServerOutput(*existing)}, nil
}

func (h *Handler) DeleteServer(ctx context.Context, input *IDInput) (*struct{}, error) {
	if err := h.store.DeleteServer(ctx, input.ID); err != nil {
		return nil, huma.Error500InternalServerError("delete server", err)
	}
	return nil, nil
}

func (h *Handler) ListTokens(ctx context.Context, _ *struct{}) (*TokensOutput, error) {
	tokens, err := h.store.ListTokens(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("list tokens", err)
	}
	out := &TokensOutput{}
	for _, t := range tokens {
		out.Body = append(out.Body, TokenOutput{ID: t.ID, Name: t.Name, CreatedAt: t.CreatedAt.Format("2006-01-02T15:04:05Z")})
	}
	if out.Body == nil {
		out.Body = []TokenOutput{}
	}
	return out, nil
}

type CreateTokenOutput struct {
	Body struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Token     string `json:"token"`
		CreatedAt string `json:"created_at"`
	}
}

func (h *Handler) CreateToken(ctx context.Context, input *CreateTokenInput) (*CreateTokenOutput, error) {
	result, err := h.store.CreateToken(ctx, input.Body.Name)
	if err != nil {
		return nil, huma.Error500InternalServerError("create token", err)
	}
	out := &CreateTokenOutput{}
	out.Body.ID = result.ID
	out.Body.Name = result.Name
	out.Body.Token = result.RawToken
	out.Body.CreatedAt = result.CreatedAt.Format("2006-01-02T15:04:05Z")
	return out, nil
}

func (h *Handler) DeleteToken(ctx context.Context, input *IDInput) (*struct{}, error) {
	if err := h.store.DeleteToken(ctx, input.ID); err != nil {
		return nil, huma.Error500InternalServerError("delete token", err)
	}
	return nil, nil
}

func (h *Handler) Bootstrap(ctx context.Context, input *BootstrapInput) (*BootstrapOutput, error) {
	// Check if any tokens already exist
	tokens, err := h.store.ListTokens(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("check tokens", err)
	}
	if len(tokens) > 0 {
		return nil, huma.Error403Forbidden("bootstrap only available when no tokens exist")
	}

	// Create first admin token
	result, err := h.store.CreateToken(ctx, input.Body.Name)
	if err != nil {
		return nil, huma.Error500InternalServerError("create token", err)
	}

	out := &BootstrapOutput{}
	out.Body.ID = result.ID
	out.Body.Name = result.Name
	out.Body.Token = result.RawToken // Only show full token once
	out.Body.CreatedAt = result.CreatedAt.Format("2006-01-02T15:04:05Z")
	return out, nil
}
