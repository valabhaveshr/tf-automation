// Package data is WS-A's Data Layer: Postgres-backed repository access for the
// tables defined in plan/ARCHITECTURE.md § Phase 1 -> Data model
// (module_schemas, records, module_selections, users, customers,
// naming_templates, naming_counters).
//
// Struct JSON tags mirror the field names used by contracts/openapi.yaml
// where the two overlap, so the API layer (WS-G) can serialize these types
// (or types embedding them) directly where convenient.
package data

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// ModuleSchema mirrors the module_schemas table: a cached, normalized
// variable schema for one module at one pinned git tag.
type ModuleSchema struct {
	ID         uuid.UUID       `json:"id"`
	ModuleName string          `json:"module_name"`
	VersionTag string          `json:"version_tag"`
	Metadata   json.RawMessage `json:"metadata"`  // parsed metadata.yaml
	Variables  json.RawMessage `json:"variables"` // normalized VarSchema[] (see contracts/var-schema.schema.json)
	ParsedAt   time.Time       `json:"parsed_at"`
	IsLatest   bool            `json:"is_latest"`
}

// Record mirrors the records table. One record = one customer/project/env
// combination = one future Terraform state.
type Record struct {
	ID            uuid.UUID  `json:"id"`
	CustomerName  string     `json:"customer_name"`
	CustomerID    *uuid.UUID `json:"customer_id,omitempty"` // join key into customers; nullable for pre-registry rows
	ProjectName   string     `json:"project_name"`
	EnvName       string     `json:"env_name"`
	OutputPath    string     `json:"output_path"` // slugified customer/project/env
	IsActive      bool       `json:"is_active"`   // soft-delete flag
	CreatedBy     string     `json:"created_by"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedBy     string     `json:"updated_by"`
	UpdatedAt     time.Time  `json:"updated_at"` // also the optimistic-concurrency token (If-Match)
	LastCommitSHA *string    `json:"last_commit_sha,omitempty"`
	SyncStatus    string     `json:"sync_status"` // pending | synced | failed
}

// Sync status values for records.sync_status.
const (
	SyncStatusPending = "pending"
	SyncStatusSynced  = "synced"
	SyncStatusFailed  = "failed"
)

// ModuleSelection mirrors the module_selections table: one selected module
// type within a record, with its instance map (becomes the for_each map at
// codegen time).
type ModuleSelection struct {
	ID             uuid.UUID       `json:"id"`
	RecordID       uuid.UUID       `json:"record_id"`
	ModuleName     string          `json:"module_name"`
	PinnedVersion  string          `json:"pinned_version"` // git tag pinned at submission time
	ModuleSchemaID uuid.UUID       `json:"module_schema_id"`
	Instances      json.RawMessage `json:"instances"` // { "web-01": {...}, "web-02": {...} }
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

// User mirrors the users table. PasswordHash is deliberately excluded from
// JSON output (json:"-") so a *User is never accidentally serialized with
// its hash back over the API.
type User struct {
	ID           uuid.UUID `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	IsAdmin      bool      `json:"is_admin"`
	CreatedAt    time.Time `json:"created_at"`

	// Added by Phase 3 (P3-A / migration 000002). Not yet populated by
	// users.go's userColumns/scanUser — that file is out of P3-A's scope, so
	// wiring these up (SELECT sso_subject, auth_provider; populating Roles
	// via GetUserRoles) is left to whichever workstream next touches
	// users.go. Roles is the role-membership source of truth going forward;
	// IsAdmin above is retained only for Phase 1 read compatibility.
	Roles        []string `json:"roles,omitempty"`
	AuthProvider string   `json:"auth_provider,omitempty"` // "local" | "oidc"
	SSOSubject   *string  `json:"sso_subject,omitempty"`   // OIDC 'sub' claim; nil for local accounts

	// CustomerID is the tenancy key (migration 000003): the one customer
	// whose resources this user can see. nil for platform admins, and for a
	// customer admin who hasn't completed onboarding yet.
	CustomerID *uuid.UUID `json:"customer_id,omitempty"`
	Email      string     `json:"email,omitempty"`
}

// Well-known role names, matching the rows seeded by migration 000002 and
// the User.roles enum in contracts/openapi.yaml.
const (
	RoleRequester = "requester" // baseline role; every authenticated user has it
	RoleAdmin     = "admin"     // superset of Phase 1's users.is_admin
	RoleApprover  = "approver"  // reserved/unused until Phase 2 resumes

	// RoleCustomerAdmin administers one customer: completes onboarding,
	// edits the customer profile, invites that customer's users. Unlike
	// RoleAdmin it never grants access outside the user's own customer.
	RoleCustomerAdmin = "customer_admin"
)

// Role mirrors the roles table: one of the three fixed, seeded roles a user
// can be granted membership in via user_roles.
type Role struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// AuditLogEntry mirrors the audit_log table: an immutable record of an
// admin-relevant action, queried via GET /admin/audit-log
// (contracts/openapi.yaml).
type AuditLogEntry struct {
	ID            uuid.UUID       `json:"id"`
	ActorUserID   *uuid.UUID      `json:"actor_user_id,omitempty"` // nullable: row survives actor deletion
	ActorUsername string          `json:"actor_username"`          // denormalized, durable even if the user row is gone
	Action        string          `json:"action"`                  // e.g. "record.create"
	ResourceType  string          `json:"resource_type"`           // record | catalog | naming_template | customer
	ResourceID    *string         `json:"resource_id,omitempty"`
	Detail        json.RawMessage `json:"detail,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
}

// Customer mirrors the customers table: the business_short_code registry
// used once-per-customer and reused across that customer's records.
type Customer struct {
	ID                uuid.UUID `json:"id"`
	Name              string    `json:"name"`
	BusinessShortCode string    `json:"business_short_code"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`

	// Profile captured at onboarding (migration 000003). All optional
	// except Name and BusinessShortCode.
	ContactName       string `json:"contact_name"`
	ContactEmail      string `json:"contact_email"`
	ContactPhone      string `json:"contact_phone"`
	Country           string `json:"country"`
	Industry          string `json:"industry"`
	DefaultCloud      string `json:"default_cloud"`
	DefaultRegion     string `json:"default_region"`
	GCPOrganizationID string `json:"gcp_organization_id"`
	BillingAccountID  string `json:"billing_account_id"`
}

// Invitation mirrors the invitations table. Only TokenHash is stored; the
// raw token exists once, in the invite link handed to the inviter.
type Invitation struct {
	ID             uuid.UUID  `json:"id"`
	TokenHash      string     `json:"-"`
	Email          string     `json:"email"`
	Role           string     `json:"role"`                  // RoleCustomerAdmin | RoleRequester
	CustomerID     *uuid.UUID `json:"customer_id,omitempty"` // nil = new customer, onboarding on first login
	InvitedBy      string     `json:"invited_by"`
	CreatedAt      time.Time  `json:"created_at"`
	ExpiresAt      time.Time  `json:"expires_at"`
	AcceptedAt     *time.Time `json:"accepted_at,omitempty"`
	AcceptedUserID *uuid.UUID `json:"accepted_user_id,omitempty"`
	RevokedAt      *time.Time `json:"revoked_at,omitempty"`
}

// NamingTemplate mirrors the naming_templates table: an admin-managed
// template for computing a compliant resource name for one (cloud,
// resource_type) pair.
type NamingTemplate struct {
	ID           uuid.UUID `json:"id"`
	Cloud        string    `json:"cloud"` // gcp | aws | azure
	ResourceType string    `json:"resource_type"`
	Template     string    `json:"template"` // e.g. "prj-{business_short_code}-{env}-{user_label}-{num_id}"
	NumIDWidth   int       `json:"num_id_width"`
	CreatedBy    string    `json:"created_by"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// NamingCounter mirrors the naming_counters table: the per-
// (business_short_code, env_name, resource_type) monotonic num_id sequence.
// Not exposed directly over the API; surfaced through AllocateNamingCounter.
type NamingCounter struct {
	ID                uuid.UUID `json:"id"`
	BusinessShortCode string    `json:"business_short_code"`
	EnvName           string    `json:"env_name"`
	ResourceType      string    `json:"resource_type"`
	NextValue         int       `json:"next_value"`
}
