package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/mcpfleet/mcpfleet/pkg/skillspec"

	"github.com/mcpfleet/registry/internal/db"
)

func registerSkillRoutes(api huma.API, h *Handler) {
	huma.Register(api, huma.Operation{
		OperationID: "list-skills",
		Method:      http.MethodGet,
		Path:        "/v1/skills",
		Summary:     "List skills",
		Tags:        []string{"skills"},
	}, h.ListSkills)

	huma.Register(api, huma.Operation{
		OperationID: "get-skill",
		Method:      http.MethodGet,
		Path:        "/v1/skills/{name}",
		Summary:     "Get a skill's frontmatter and sha256",
		Tags:        []string{"skills"},
	}, h.GetSkill)

	huma.Register(api, huma.Operation{
		OperationID: "get-skill-archive",
		Method:      http.MethodGet,
		Path:        "/v1/skills/{name}/archive",
		Summary:     "Download a skill's tar.gz archive",
		Description: "Returns the archive as uploaded; the X-Skill-Sha256 header carries the skill hash.",
		Tags:        []string{"skills"},
	}, h.GetSkillArchive)

	huma.Register(api, huma.Operation{
		OperationID: "put-skill",
		Method:      http.MethodPut,
		Path:        "/v1/skills/{name}",
		Summary:     "Create or replace a skill",
		Description: fmt.Sprintf("The body is a tar.gz archive holding one directory, {name}, with the skill's SKILL.md. "+
			"The registry validates it against the Agent Skills specification and returns 422 with the violations, "+
			"422 when metadata.mcpfleet-requires names a server the registry does not have, and 413 above %d bytes. "+
			"The sha256 is computed by the registry.", MaxSkillArchiveBytes),
		Tags: []string{"skills"},
		// Huma rejects a body of exactly MaxBodyBytes, so allow one more.
		MaxBodyBytes: MaxSkillArchiveBytes + 1,
	}, h.PutSkill)

	huma.Register(api, huma.Operation{
		OperationID: "delete-skill",
		Method:      http.MethodDelete,
		Path:        "/v1/skills/{name}",
		Summary:     "Delete a skill",
		Tags:        []string{"skills"},
	}, h.DeleteSkill)
}

// --- DTOs ---

type SkillOutput struct {
	Name          string            `json:"name"`
	Description   string            `json:"description"`
	License       string            `json:"license"`
	Compatibility string            `json:"compatibility"`
	AllowedTools  string            `json:"allowed_tools"`
	Metadata      map[string]string `json:"metadata"`
	Requires      []string          `json:"requires"`
	SHA256        string            `json:"sha256"`
	Size          int64             `json:"size" doc:"Size of the tar.gz archive in bytes"`
	CreatedAt     string            `json:"created_at"`
	UpdatedAt     string            `json:"updated_at"`
}

// SkillViolation is a rule of the Agent Skills specification a skill breaks.
type SkillViolation struct {
	Field   string `json:"field"`
	Rule    string `json:"rule"`
	Message string `json:"message"`
}

type SkillsOutput struct {
	Body []SkillOutput
}

type SingleSkillOutput struct {
	Body SkillOutput
}

type PutSkillOutput struct {
	Body struct {
		SkillOutput
		// Warnings are the specification's recommendations the skill does
		// not follow; they do not block the upload.
		Warnings []SkillViolation `json:"warnings"`
	}
}

type SkillArchiveOutput struct {
	ContentType string `header:"Content-Type"`
	SHA256      string `header:"X-Skill-Sha256"`
	Body        []byte
}

type SkillNameInput struct {
	Name string `path:"name"`
}

// Resolve decodes a percent-encoded {name} the way IDInput.Resolve does.
func (i *SkillNameInput) Resolve(ctx huma.Context) []error {
	if ctx.URL().RawPath == "" {
		return nil
	}
	v, err := url.PathUnescape(i.Name)
	if err != nil {
		return []error{&huma.ErrorDetail{Location: "path.name", Message: err.Error(), Value: i.Name}}
	}
	i.Name = v
	return nil
}

type PutSkillInput struct {
	SkillNameInput
	RawBody []byte `contentType:"application/gzip"`
}

// serverRequiredError is the 409 of a server delete that skills block; it
// lists the skills in a skills field besides the usual problem details.
type serverRequiredError struct {
	huma.ErrorModel
	Skills []string `json:"skills"`
}

func toSkillOutput(s db.Skill) SkillOutput {
	metadata := s.Metadata
	if metadata == nil {
		metadata = map[string]string{}
	}
	requires := s.Requires
	if requires == nil {
		requires = []string{}
	}
	return SkillOutput{
		Name:          s.Name,
		Description:   s.Description,
		License:       s.License,
		Compatibility: s.Compatibility,
		AllowedTools:  s.AllowedTools,
		Metadata:      metadata,
		Requires:      requires,
		SHA256:        s.SHA256,
		Size:          s.Size,
		CreatedAt:     s.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt:     s.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	}
}

// usableSkillDirName reports whether name can be the single directory the
// archive is unpacked into. Every other rule on the name is skillspec's.
func usableSkillDirName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, "/\\\x00")
}

// --- Handlers ---

func (h *Handler) ListSkills(ctx context.Context, _ *struct{}) (*SkillsOutput, error) {
	skills, err := h.store.ListSkills(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("list skills", err)
	}
	out := &SkillsOutput{Body: []SkillOutput{}}
	for _, s := range skills {
		out.Body = append(out.Body, toSkillOutput(s))
	}
	return out, nil
}

func (h *Handler) GetSkill(ctx context.Context, input *SkillNameInput) (*SingleSkillOutput, error) {
	s, err := h.store.GetSkill(ctx, input.Name)
	if errors.Is(err, db.ErrNotFound) {
		return nil, huma.Error404NotFound("skill not found")
	}
	if err != nil {
		return nil, huma.Error500InternalServerError("get skill", err)
	}
	return &SingleSkillOutput{Body: toSkillOutput(*s)}, nil
}

func (h *Handler) GetSkillArchive(ctx context.Context, input *SkillNameInput) (*SkillArchiveOutput, error) {
	archive, sha, err := h.store.GetSkillArchive(ctx, input.Name)
	if errors.Is(err, db.ErrNotFound) {
		return nil, huma.Error404NotFound("skill not found")
	}
	if err != nil {
		return nil, huma.Error500InternalServerError("get skill archive", err)
	}
	return &SkillArchiveOutput{ContentType: "application/gzip", SHA256: sha, Body: archive}, nil
}

// PutSkill unpacks the archive into a temporary directory, validates it with
// skillspec (its errors are the 422 violations, its warnings are returned),
// computes the hash and stores the skill if every required server exists.
func (h *Handler) PutSkill(ctx context.Context, input *PutSkillInput) (*PutSkillOutput, error) {
	if !usableSkillDirName(input.Name) {
		return nil, huma.Error422UnprocessableEntity("invalid skill name",
			&huma.ErrorDetail{Location: "path.name", Message: `must be one path segment, not "." or ".."`, Value: input.Name})
	}

	tmp, err := os.MkdirTemp("", "registry-skill-*")
	if err != nil {
		return nil, huma.Error500InternalServerError("unpack skill", err)
	}
	defer os.RemoveAll(tmp)

	dir, err := unpackSkillArchive(input.RawBody, input.Name, tmp)
	if err != nil {
		var ae *archiveError
		if errors.As(err, &ae) {
			return nil, huma.Error422UnprocessableEntity("invalid skill archive",
				&huma.ErrorDetail{Location: "body", Message: ae.Error()})
		}
		return nil, huma.Error500InternalServerError("unpack skill", err)
	}

	res := skillspec.Validate(dir)
	if !res.Valid() {
		details := make([]error, 0, len(res.Errors))
		for _, v := range res.Errors {
			details = append(details, &huma.ErrorDetail{Location: v.Field, Message: v.Message + " (" + v.Rule + ")"})
		}
		return nil, huma.Error422UnprocessableEntity("skill does not conform to the Agent Skills specification", details...)
	}

	parsed, err := skillspec.Parse(dir)
	if err != nil {
		return nil, huma.Error422UnprocessableEntity("invalid skill",
			&huma.ErrorDetail{Location: "body", Message: err.Error()})
	}
	skill := &db.Skill{
		Name:          input.Name,
		Description:   parsed.Description,
		License:       parsed.License,
		Compatibility: parsed.Compatibility,
		AllowedTools:  parsed.AllowedTools,
		Metadata:      parsed.Metadata,
		Requires:      parsed.Requires,
		SHA256:        parsed.SHA256,
	}
	if _, err := h.store.UpsertSkill(ctx, skill, input.RawBody); err != nil {
		var missing *db.MissingServersError
		if errors.As(err, &missing) {
			details := make([]error, 0, len(missing.Servers))
			for _, name := range missing.Servers {
				details = append(details, &huma.ErrorDetail{
					Location: "metadata." + skillspec.MetadataRequiresKey,
					Message:  fmt.Sprintf("server %q is not in the registry", name),
					Value:    name,
				})
			}
			return nil, huma.Error422UnprocessableEntity("skill requires servers the registry does not have", details...)
		}
		return nil, huma.Error500InternalServerError("store skill", err)
	}

	out := &PutSkillOutput{}
	out.Body.SkillOutput = toSkillOutput(*skill)
	out.Body.Warnings = []SkillViolation{}
	for _, w := range res.Warnings {
		out.Body.Warnings = append(out.Body.Warnings, SkillViolation{Field: w.Field, Rule: w.Rule, Message: w.Message})
	}
	return out, nil
}

func (h *Handler) DeleteSkill(ctx context.Context, input *SkillNameInput) (*struct{}, error) {
	if err := h.store.DeleteSkill(ctx, input.Name); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil, huma.Error404NotFound("skill not found")
		}
		return nil, huma.Error500InternalServerError("delete skill", err)
	}
	return nil, nil
}
