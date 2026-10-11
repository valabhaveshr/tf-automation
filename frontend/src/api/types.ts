/**
 * Hand-written TypeScript mirror of contracts/openapi.yaml's component schemas
 * and contracts/var-schema.schema.json, kept name-aligned so a future
 * OpenAPI-generated client is a drop-in replacement for this file (and
 * api/client.ts's call sites don't need to change shape, just origin).
 *
 * Source of truth: contracts/openapi.yaml + contracts/var-schema.schema.json.
 * Any change to those contracts should be mirrored here by hand until a
 * generator is wired up.
 *
 * Naming: the UI calls a record a "resource" (one customer/project/env
 * request). The API and these types keep the original "record" names.
 */

// ---------------------------------------------------------------------------
// contracts/var-schema.schema.json — VarSchema
// ---------------------------------------------------------------------------

export type VarSchemaType =
  | 'string'
  | 'number'
  | 'bool'
  | 'list'
  | 'set'
  | 'map'
  | 'object'
  | 'tuple'
  | 'any'
  | 'unsupported';

export interface VarSchemaValidation {
  condition_raw: string;
  error_message: string;
}

export interface VarSchema {
  name: string;
  type: VarSchemaType;
  type_raw: string;
  /** For list/set/map: the VarSchema-shaped type of elements. Omitted for scalars/object. */
  element_type?: VarSchema;
  /** For type="object": field name -> VarSchema (recursive). */
  object_fields?: Record<string, VarSchema>;
  /** Present only when has_default is true. Do not infer has_default from this field's presence. */
  default?: string | number | boolean | object | unknown[] | null;
  has_default: boolean;
  required: boolean;
  description?: string;
  sensitive: boolean;
  nullable: boolean;
  validations: VarSchemaValidation[];
}

// ---------------------------------------------------------------------------
// contracts/openapi.yaml — components.schemas
// ---------------------------------------------------------------------------

export type UserRole = 'requester' | 'admin' | 'approver' | 'customer_admin';

export interface User {
  id: string;
  username: string;
  auth_provider: 'local' | 'oidc';
  roles: UserRole[];
  /** True for platform admins only (admin role, no customer). */
  is_admin: boolean;
}

export type CloudProvider = 'aws' | 'azure' | 'gcp' | 'multi';

export interface Customer {
  id: string;
  name: string;
  /** Unique short name, stored lowercase (e.g. "dmsc"); shown uppercase. */
  business_short_code: string;
  contact_name: string;
  contact_email: string;
  contact_phone: string;
  country: string;
  industry: string;
  default_cloud: string;
  default_region: string;
  gcp_organization_id: string;
  billing_account_id: string;
  created_at: string;
}

/** Onboarding / organization profile form. */
export type CustomerProfileInput = Omit<Customer, 'id' | 'created_at'>;

/** GET /auth/me */
export interface Me {
  user: User;
  customer: Customer | null;
  needs_onboarding: boolean;
  is_platform_admin: boolean;
  is_customer_admin: boolean;
}

export interface TeamMember {
  id: string;
  username: string;
  email: string;
  roles: UserRole[];
  created_at: string;
}

export type InvitationRole = 'customer_admin' | 'requester';
export type InvitationStatus = 'pending' | 'accepted' | 'revoked' | 'expired';

export interface Invitation {
  id: string;
  email: string;
  role: InvitationRole;
  customer_id?: string;
  customer_name?: string;
  invited_by: string;
  created_at: string;
  expires_at: string;
  status: InvitationStatus;
  /** Only present in the create response: "/invite/<token>". */
  invite_path?: string;
}

export interface InvitationPreview {
  email: string;
  role: InvitationRole;
  /** Empty for a new customer (onboarding follows). */
  customer_name?: string;
  expires_at: string;
}

export type NamingTemplateCloud = 'gcp' | 'aws' | 'azure';

export interface NamingTemplate {
  cloud: NamingTemplateCloud;
  resource_type: string;
  template: string;
  num_id_width?: number;
}

export interface CatalogEntry {
  module_name: string;
  display_name: string;
  category: string;
  cloud: CloudProvider;
  icon: string;
  description: string;
  latest_version_tag: string;
  naming_resource_type: string | null;
}

/** One entry of metadata.yaml's `variables:` section, in form order. */
export interface VariableHint {
  name: string;
  /** Shown verbatim on the "e" hover. */
  example?: string;
  /** Form section heading. */
  group?: string;
  /** Collapsed under "Advanced settings" by default. */
  advanced?: boolean;
}

/** Parsed metadata.yaml, per plan/ARCHITECTURE.md "Module authoring convention". */
export interface ModuleMetadata {
  name: string;
  display_name: string;
  category: string;
  cloud: CloudProvider;
  icon: string;
  description: string;
  instance_label_field: string;
  naming_resource_type?: string | null;
  /** Which variable receives the computed name; defaults to instance_label_field if set. */
  naming_target_variable?: string | null;
  variables?: VariableHint[];
}

export interface ModuleSchema {
  module_name: string;
  version_tag: string;
  metadata: ModuleMetadata;
  variables: VarSchema[];
}

/** Map keyed by instance name, e.g. {"web-01": {...}}. Values are raw form values per variable name. */
export type InstanceValues = Record<string, unknown>;
export type InstancesMap = Record<string, InstanceValues>;

export interface ModuleInstanceInput {
  module_name: string;
  /** Omit on create to pin the latest tag at submission time; set on edit to keep the existing pin. */
  pinned_version?: string | null;
  instances: InstancesMap;
}

export interface RecordInput {
  /** Platform admins pick the customer; ignored for customer users. */
  customer_id?: string;
  /** Overwritten server-side with the customer's registry name. */
  customer_name: string;
  project_name: string;
  env_name: string;
  module_selections: ModuleInstanceInput[];
}

export interface PreviewResult {
  /** Relative path -> file content, e.g. "main.tf", "compute-instance.auto.tfvars", "backend.tf". */
  files: Record<string, string>;
  /** "module_name.instance_name" -> tentative Naming Convention Engine suggestion (not yet allocated). */
  suggested_names: Record<string, string>;
  /** Unified diff against the currently committed version, present only for edit-preview. */
  diff?: string | null;
  warnings?: string[];
}

export type SyncStatus = 'pending' | 'synced' | 'failed';

export interface RecordSummary {
  id: string;
  customer_id?: string;
  customer_name: string;
  project_name: string;
  env_name: string;
  output_path: string;
  is_active: boolean;
  sync_status: SyncStatus;
  updated_at: string;
}

// Named TfRecord, not Record: a type named Record here would shadow the built-in
// Record<K, V> utility type for this whole module.
export interface TfRecord extends RecordSummary {
  module_selections: ModuleInstanceInput[];
  last_commit_sha: string | null;
  created_by: string;
  created_at: string;
  updated_by: string;
  warnings?: string[];
}

export interface SyncStatusResponse {
  sync_status: SyncStatus;
  last_commit_sha: string | null;
}

export interface LoginRequest {
  username: string;
  password: string;
}

export interface LoginResponse {
  user: User;
}

// ---------------------------------------------------------------------------
// contracts/openapi.yaml — AuditLogEntry (Phase 3: /admin/audit-log)
// ---------------------------------------------------------------------------

export type AuditLogResourceType = 'record' | 'catalog' | 'naming_template' | 'customer' | 'invitation';

export interface AuditLogEntry {
  id: string;
  actor_user_id: string | null;
  actor_username: string;
  /** e.g. "record.create" */
  action: string;
  resource_type: AuditLogResourceType;
  resource_id: string | null;
  detail: Record<string, unknown> | null;
  created_at: string;
}
