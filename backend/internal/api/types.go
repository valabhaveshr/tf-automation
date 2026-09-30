// Package api is WS-G: the Backend API orchestration layer. It wires WS-A
// (data.Store), WS-C (codegen), WS-D (catalog.Service), WS-E
// (output.Client), WS-F (auth.Sessions), and WS-I (naming.Engine) together
// into the HTTP API frozen by contracts/openapi.yaml. See
// plan/ARCHITECTURE.md § Phase 1 technical design detail (the transaction-
// flow sequence diagram in particular) for the exact flow this package
// implements.
package api

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------
// Response/request DTOs mirroring contracts/openapi.yaml's component
// schemas. Kept local to this package (rather than reusing WS-A's data.*
// types directly as wire shapes) so the wire contract and the storage
// schema can evolve independently, per the frozen OpenAPI contract being
// the authoritative shape here.
// ---------------------------------------------------------------------

// userResponse mirrors #/components/schemas/User, used by GET /auth/me
// (auth_me.go). Note internal/auth/login.go's POST /auth/login response
// has its own identically-shaped userResponse type local to that package,
// for the decoupling reasons documented there.
type userResponse struct {
	ID           uuid.UUID `json:"id"`
	Username     string    `json:"username"`
	AuthProvider string    `json:"auth_provider"`
	Roles        []string  `json:"roles"`
	IsAdmin      bool      `json:"is_admin"`
}

type customerResponse struct {
	ID                uuid.UUID `json:"id"`
	Name              string    `json:"name"`
	BusinessShortCode string    `json:"business_short_code"`
}

type namingTemplateResponse struct {
	Cloud        string `json:"cloud"`
	ResourceType string `json:"resource_type"`
	Template     string `json:"template"`
	NumIDWidth   int    `json:"num_id_width"`
}

type namingTemplateRequest struct {
	Cloud        string `json:"cloud"`
	ResourceType string `json:"resource_type"`
	Template     string `json:"template"`
	NumIDWidth   int    `json:"num_id_width"`
}

type catalogEntry struct {
	ModuleName          string  `json:"module_name"`
	DisplayName         string  `json:"display_name"`
	Category            string  `json:"category"`
	Cloud               string  `json:"cloud"`
	Icon                string  `json:"icon"`
	Description         string  `json:"description"`
	LatestVersionTag    string  `json:"latest_version_tag"`
	NamingResourceType  *string `json:"naming_resource_type"`
}

type moduleSchemaResponse struct {
	ModuleName string          `json:"module_name"`
	VersionTag string          `json:"version_tag"`
	Metadata   json.RawMessage `json:"metadata"`
	Variables  json.RawMessage `json:"variables"`
}

// moduleInstanceInput mirrors #/components/schemas/ModuleInstanceInput. Used
// both as request input (RecordInput.module_selections) and as part of the
// Record response.
type moduleInstanceInput struct {
	ModuleName    string                            `json:"module_name"`
	PinnedVersion *string                            `json:"pinned_version,omitempty"`
	Instances     map[string]map[string]any          `json:"instances"`
}

// recordInput mirrors #/components/schemas/RecordInput, the request body
// shared by preview/create/update.
type recordInput struct {
	CustomerName     string                 `json:"customer_name"`
	ProjectName      string                 `json:"project_name"`
	EnvName          string                 `json:"env_name"`
	ModuleSelections []moduleInstanceInput  `json:"module_selections"`
}

// previewResult mirrors #/components/schemas/PreviewResult. Warnings is an
// additive, non-contract-breaking field (OpenAPI schemas are open by
// default) carrying non-fatal naming issues -- e.g.
// naming.ErrNoTemplateConfigured -- which per plan/ARCHITECTURE.md's
// naming-convention edge case table must NOT block preview/save.
type previewResult struct {
	Files          map[string]string `json:"files"`
	SuggestedNames map[string]string `json:"suggested_names"`
	Diff           *string           `json:"diff"`
	Warnings       []string          `json:"warnings,omitempty"`
}

// recordSummary mirrors #/components/schemas/RecordSummary.
type recordSummary struct {
	ID           uuid.UUID `json:"id"`
	CustomerName string    `json:"customer_name"`
	ProjectName  string    `json:"project_name"`
	EnvName      string    `json:"env_name"`
	OutputPath   string    `json:"output_path"`
	IsActive     bool      `json:"is_active"`
	SyncStatus   string    `json:"sync_status"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// recordResponse mirrors #/components/schemas/Record (RecordSummary allOf
// the extra fields).
type recordResponse struct {
	recordSummary
	ModuleSelections []moduleInstanceInput `json:"module_selections"`
	LastCommitSHA    *string                `json:"last_commit_sha,omitempty"`
	CreatedBy        string                 `json:"created_by"`
	CreatedAt        time.Time              `json:"created_at"`
	UpdatedBy        string                 `json:"updated_by"`
	Warnings         []string               `json:"warnings,omitempty"`
}

type syncStatusResponse struct {
	SyncStatus    string  `json:"sync_status"`
	LastCommitSHA *string `json:"last_commit_sha"`
}
