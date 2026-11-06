package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"unicode"

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
		Description: "Returns 404 for an unknown token and 409 when the token is the last one left; create a new token before deleting the old one.",
		Tags:        []string{"tokens"},
	}, h.DeleteToken)

	// Bootstrap - create first admin token without auth
	huma.Register(api, huma.Operation{
		OperationID: "bootstrap",
		Method:      http.MethodPost,
		Path:        "/bootstrap",
		Summary:     "Create first admin token (no auth required)",
		Description: "Works once in the life of the registry database. Once any token has been created, it returns 403, even if every token is later deleted.",
		Tags:        []string{"bootstrap"},
	}, h.Bootstrap)
}

// --- DTOs ---

type ServerOutput struct {
	ID          string               `json:"id"`
	Name        string               `json:"name"`
	Description string               `json:"description"`
	Transport   string               `json:"transport"`
	Install     map[string]string    `json:"install"`
	Command     string               `json:"command"`
	URL         string               `json:"url"`
	Args        []string             `json:"args"`
	Env         map[string]db.EnvVar `json:"env"`
	Headers     map[string]db.EnvVar `json:"headers"`
	Tags        []string             `json:"tags"`
	Platforms   []string             `json:"platforms"`
	CreatedAt   string               `json:"created_at"`
	UpdatedAt   string               `json:"updated_at"`
}

type ServersOutput struct {
	Body []ServerOutput
}

// ServerBody is the request body of create and update. Huma treats every
// field without omitempty as required, so only name is mandatory in the
// schema; Resolve adds the transport rules for command and url.
type ServerBody struct {
	Name        string               `json:"name" required:"true" minLength:"1"`
	Description string               `json:"description,omitempty"`
	Transport   string               `json:"transport,omitempty"`
	Install     map[string]string    `json:"install,omitempty"`
	Command     string               `json:"command,omitempty"`
	URL         string               `json:"url,omitempty"`
	Args        []string             `json:"args,omitempty"`
	Env         map[string]db.EnvVar `json:"env,omitempty"`
	Headers     map[string]db.EnvVar `json:"headers,omitempty"`
	Tags        []string             `json:"tags,omitempty"`
	Platforms   []string             `json:"platforms,omitempty"`
}

// Resolve checks the name rules (see validateName), that command and url
// fit the transport (a stdio server, the default when transport is empty,
// runs a command; an sse or http server is reached at an absolute http(s)
// url), and the header rules of a remote server (see validateHeaders). Huma
// calls it after the schema validation and reports every returned error as
// one errors[] entry of a 422 response.
func (b *ServerBody) Resolve(_ huma.Context, prefix *huma.PathBuffer) []error {
	problem := func(field, message string, value any) error {
		return &huma.ErrorDetail{Location: prefix.With(field), Message: message, Value: value}
	}
	var errs []error
	for _, message := range validateName(b.Name) {
		errs = append(errs, problem("name", message, b.Name))
	}
	switch b.Transport {
	case "", "stdio":
		if b.Command == "" {
			errs = append(errs, problem("command", "command is required for transport stdio", b.Command))
		}
		if b.URL != "" {
			errs = append(errs, problem("url", "url is only used for transport http or sse", b.URL))
		}
		if len(b.Headers) > 0 {
			errs = append(errs, problem("headers", "headers is only used for transport http or sse", nil))
		}
	case "sse", "http":
		if b.URL == "" {
			errs = append(errs, problem("url", "url is required for transport "+b.Transport, b.URL))
		} else if !isHTTPURL(b.URL) {
			errs = append(errs, problem("url", "url must be an absolute http or https URL", b.URL))
		}
		if b.Command != "" {
			errs = append(errs, problem("command", "command is not used for transport "+b.Transport+"; remove it", b.Command))
		}
		// Header problems carry no value: it may be a credential, which
		// must not be echoed into responses and logs.
		for _, h := range validateHeaders(b.Headers) {
			errs = append(errs, problem("headers."+h.name, h.message, nil))
		}
	default:
		errs = append(errs, problem("transport", "transport must be stdio, sse or http", b.Transport))
	}
	return errs
}

// validateName returns why name cannot be a server name, or nil. A name must
// work as one segment of /v1/servers/{id}: "." and ".." are dot segments that
// clients and proxies remove when they normalize a path (RFC 3986 §5.2.4),
// so such a server could not be read, updated or deleted. Control characters
// (C0, DEL, C1) break terminal output and agent config keys; the message
// names the first one by code point, since it is invisible in the JSON value.
// Only create and update check names, so a stored server with an old invalid
// name can still be deleted.
func validateName(name string) []string {
	var problems []string
	switch {
	case name == "." || name == "..":
		problems = append(problems, `name must not be "." or ".."`)
	case strings.TrimSpace(name) == "":
		problems = append(problems, "name must not be empty or only whitespace")
	}
	for i, r := range name {
		if unicode.IsControl(r) {
			problems = append(problems, fmt.Sprintf("name must not contain control characters (found U+%04X at byte %d)", r, i))
			break
		}
	}
	return problems
}

// headerProblem is one rule a header of a server body breaks.
type headerProblem struct {
	name    string
	message string
}

// credentialHeaders carry credentials, so their value must be a secret
// reference: the registry stores references, never secret values. Keys are
// lower case.
var credentialHeaders = map[string]bool{
	"authorization":       true,
	"proxy-authorization": true,
	"cookie":              true,
}

// validateHeaders returns the problems of the headers of a remote server, in
// the order of the sorted header names so responses are stable. A name must
// be an HTTP token (RFC 9110 §5.6.2) and unique ignoring case, the second of
// two names that differ only in case being the reported one. An entry holds
// exactly one of value and secret; a value must not contain CR, LF or NUL,
// which would split or end the header line in the agent's request; and a
// credential header must use a secret.
func validateHeaders(headers map[string]db.EnvVar) []headerProblem {
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	sort.Strings(names)
	var problems []headerProblem
	seen := map[string]bool{}
	for _, name := range names {
		h := headers[name]
		lower := strings.ToLower(name)
		switch {
		case !isHTTPToken(name):
			problems = append(problems, headerProblem{name, "header name must be an HTTP token"})
		case seen[lower]:
			problems = append(problems, headerProblem{name, "duplicate header (case-insensitive): " + lower})
		}
		seen[lower] = true
		switch {
		case (h.Value == "") == (h.Secret == ""):
			problems = append(problems, headerProblem{name, "header needs exactly one of value or secret"})
		case strings.ContainsAny(h.Value, "\r\n\x00"):
			problems = append(problems, headerProblem{name, "header value must not contain CR, LF or NUL"})
		case h.Value != "" && credentialHeaders[lower]:
			problems = append(problems, headerProblem{name, `use {"secret": ...} for credential headers; the registry does not store secret values`})
		}
	}
	return problems
}

// isHTTPToken reports whether s is a non-empty RFC 9110 token: ASCII letters,
// digits and !#$%&'*+-.^_`|~.
func isHTTPToken(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case strings.IndexByte("!#$%&'*+-.^_`|~", c) >= 0:
		default:
			return false
		}
	}
	return true
}

func isHTTPURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

type ServerInput struct {
	Body ServerBody
}

type IDInput struct {
	ID string `path:"id"`
}

// Resolve decodes an {id} that arrived percent-encoded. When the request path
// holds an encoded "/" (%2F), Go keeps the raw form in URL.RawPath, chi routes
// on it and humachi hands the parameter over still encoded; without RawPath
// chi has already decoded it, so decoding again would corrupt a literal "%".
func (i *IDInput) Resolve(ctx huma.Context) []error {
	if ctx.URL().RawPath == "" {
		return nil
	}
	v, err := url.PathUnescape(i.ID)
	if err != nil {
		return []error{&huma.ErrorDetail{Location: "path.id", Message: err.Error(), Value: i.ID}}
	}
	i.ID = v
	return nil
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
		env = map[string]db.EnvVar{}
	}
	headers := s.Headers
	if headers == nil {
		headers = map[string]db.EnvVar{}
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
		URL:         s.URL,
		Args:        args,
		Env:         env,
		Headers:     headers,
		Tags:        tags,
		Platforms:   platforms,
		CreatedAt:   s.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt:   s.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	}
}

// nameTakenError is the 409 returned when a server name is already used.
func nameTakenError(name string) huma.StatusError {
	return huma.Error409Conflict(fmt.Sprintf("server name %q is already taken", name))
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
		URL:         input.Body.URL,
		Args:        input.Body.Args,
		Env:         input.Body.Env,
		Headers:     input.Body.Headers,
		Tags:        input.Body.Tags,
		Platforms:   input.Body.Platforms,
	}
	if srv.Transport == "" {
		srv.Transport = "stdio"
	}
	if err := h.store.CreateServer(ctx, srv); err != nil {
		if errors.Is(err, db.ErrNameTaken) {
			return nil, nameTakenError(srv.Name)
		}
		return nil, huma.Error500InternalServerError("create server", err)
	}
	return &SingleServerOutput{Body: toServerOutput(*srv)}, nil
}

func (h *Handler) UpdateServer(ctx context.Context, input *struct {
	IDInput
	Body ServerBody
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
	existing.URL = input.Body.URL
	existing.Args = input.Body.Args
	existing.Env = input.Body.Env
	existing.Headers = input.Body.Headers
	existing.Tags = input.Body.Tags
	existing.Platforms = input.Body.Platforms
	if existing.Transport == "" {
		existing.Transport = "stdio"
	}
	if err := h.store.UpdateServer(ctx, existing); err != nil {
		if errors.Is(err, db.ErrNameTaken) {
			return nil, nameTakenError(existing.Name)
		}
		return nil, huma.Error500InternalServerError("update server", err)
	}
	return &SingleServerOutput{Body: toServerOutput(*existing)}, nil
}

func (h *Handler) DeleteServer(ctx context.Context, input *IDInput) (*struct{}, error) {
	if err := h.store.DeleteServer(ctx, input.ID); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil, huma.Error404NotFound("server not found")
		}
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
		switch {
		case errors.Is(err, db.ErrNotFound):
			return nil, huma.Error404NotFound("token not found")
		case errors.Is(err, db.ErrLastToken):
			return nil, huma.Error409Conflict("cannot delete the last token")
		}
		return nil, huma.Error500InternalServerError("delete token", err)
	}
	return nil, nil
}

func (h *Handler) Bootstrap(ctx context.Context, input *BootstrapInput) (*BootstrapOutput, error) {
	result, err := h.store.Bootstrap(ctx, input.Body.Name)
	if err != nil {
		if errors.Is(err, db.ErrAlreadyBootstrapped) {
			return nil, huma.Error403Forbidden("registry already bootstrapped; create tokens with an existing token or the registry token create command")
		}
		return nil, huma.Error500InternalServerError("bootstrap", err)
	}

	out := &BootstrapOutput{}
	out.Body.ID = result.ID
	out.Body.Name = result.Name
	out.Body.Token = result.RawToken // Only show full token once
	out.Body.CreatedAt = result.CreatedAt.Format("2006-01-02T15:04:05Z")
	return out, nil
}
