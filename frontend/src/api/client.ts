/**
 * Thin fetch-based client for the API frozen in contracts/openapi.yaml.
 * Every function name/shape below maps 1:1 to an OpenAPI path+method so this
 * file can be swapped for a generated client later without touching callers.
 *
 * All requests use credentials: 'include' so the session cookie
 * (tf_automation_session, per openapi.yaml securitySchemes.sessionAuth) is
 * sent automatically; there is no bearer-token handling needed in Phase 1.
 */
import type {
  AuditLogEntry,
  AuditLogResourceType,
  Customer,
  CatalogEntry,
  LoginRequest,
  LoginResponse,
  ModuleSchema,
  NamingTemplate,
  PreviewResult,
  TfRecord,
  RecordInput,
  RecordSummary,
  SyncStatusResponse,
} from './types';

const API_BASE = '/api';

export class ApiError extends Error {
  status: number;
  body: unknown;

  constructor(status: number, message: string, body?: unknown) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.body = body;
  }
}

interface RequestOptions {
  method?: string;
  body?: unknown;
  query?: Record<string, string | number | boolean | undefined>;
  headers?: Record<string, string>;
}

function buildUrl(path: string, query?: RequestOptions['query']): string {
  const url = new URL(API_BASE + path, window.location.origin);
  if (query) {
    for (const [key, value] of Object.entries(query)) {
      if (value !== undefined && value !== null) {
        url.searchParams.set(key, String(value));
      }
    }
  }
  return url.pathname + url.search;
}

async function request<T>(path: string, opts: RequestOptions = {}): Promise<T> {
  const res = await fetch(buildUrl(path, opts.query), {
    method: opts.method ?? 'GET',
    credentials: 'include',
    headers: {
      ...(opts.body !== undefined ? { 'Content-Type': 'application/json' } : {}),
      ...opts.headers,
    },
    body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
  });

  if (res.status === 204 || res.status === 202) {
    return undefined as T;
  }

  const isJson = res.headers.get('content-type')?.includes('application/json');
  const payload = isJson ? await res.json().catch(() => undefined) : undefined;

  if (!res.ok) {
    throw new ApiError(res.status, `${opts.method ?? 'GET'} ${path} failed with ${res.status}`, payload);
  }

  return payload as T;
}

// ---------------------------------------------------------------------------
// Auth
// ---------------------------------------------------------------------------

export function login(input: LoginRequest): Promise<LoginResponse> {
  return request<LoginResponse>('/auth/login', { method: 'POST', body: input });
}

export function logout(): Promise<void> {
  return request<void>('/auth/logout', { method: 'POST' });
}

/**
 * GET /auth/me: the current session's user, re-derived server-side from the
 * session cookie. Required so the frontend can recover identity after a
 * full-page redirect (the /auth/sso/callback flow in particular) where no
 * in-memory login response is available to read the user from -- call this
 * on mount (see state/auth.ts's AuthProvider). Throws ApiError with status
 * 401 if there is no valid session.
 */
export function getCurrentUser(): Promise<LoginResponse> {
  return request<LoginResponse>('/auth/me');
}

/**
 * Builds the URL for GET /auth/sso/login. This is a plain browser redirect
 * (per openapi.yaml, the server responds 302 to the IdP), not a fetch call —
 * navigate the browser to it directly, e.g.:
 *   window.location.assign(ssoLoginUrl('/records'))
 * `redirectTo` is validated against an allowlist server-side; omit it to use
 * the server's default post-login landing page.
 */
export function ssoLoginUrl(redirectTo?: string): string {
  const url = new URL(API_BASE + '/auth/sso/login', window.location.origin);
  if (redirectTo) {
    url.searchParams.set('redirect_to', redirectTo);
  }
  return url.pathname + url.search;
}

// ---------------------------------------------------------------------------
// Catalog
// ---------------------------------------------------------------------------

export function getCatalog(): Promise<CatalogEntry[]> {
  return request<CatalogEntry[]>('/catalog');
}

export function getModuleSchema(moduleName: string, ref?: string): Promise<ModuleSchema> {
  // Nested module names ("compute/instance") keep their slashes as path
  // separators; only each segment is encoded. Encoding the slash as %2F
  // also works against the backend, but some reverse proxies reject it.
  const modulePath = moduleName.split('/').map(encodeURIComponent).join('/');
  return request<ModuleSchema>(`/catalog/${modulePath}/schema`, {
    query: { ref },
  });
}

/** Admin-only; server returns 403 for non-admin callers. */
export function refreshCatalog(): Promise<void> {
  return request<void>('/catalog/refresh', { method: 'POST' });
}

// ---------------------------------------------------------------------------
// Admin: customers + naming templates
// ---------------------------------------------------------------------------

export function listCustomers(): Promise<Customer[]> {
  return request<Customer[]>('/admin/customers');
}

export function createCustomer(input: { name: string; business_short_code: string }): Promise<Customer> {
  return request<Customer>('/admin/customers', { method: 'POST', body: input });
}

export function listNamingTemplates(): Promise<NamingTemplate[]> {
  return request<NamingTemplate[]>('/admin/naming-templates');
}

export function upsertNamingTemplate(input: NamingTemplate): Promise<NamingTemplate> {
  return request<NamingTemplate>('/admin/naming-templates', { method: 'POST', body: input });
}

/** Admin-only; server returns 403 for callers without the admin role. */
export function getAuditLog(filter?: {
  resource_type?: AuditLogResourceType;
  resource_id?: string;
  actor?: string;
  limit?: number;
  /** Cursor — return entries created before this ISO timestamp. */
  before?: string;
}): Promise<AuditLogEntry[]> {
  return request<AuditLogEntry[]>('/admin/audit-log', { query: filter });
}

// ---------------------------------------------------------------------------
// Records
// ---------------------------------------------------------------------------

export function listRecords(params?: {
  customer?: string;
  project?: string;
  active?: boolean;
}): Promise<RecordSummary[]> {
  return request<RecordSummary[]>('/records', { query: params });
}

export function getRecord(id: string): Promise<TfRecord> {
  return request<TfRecord>(`/records/${encodeURIComponent(id)}`);
}

/** Validate + generate without persisting or committing. */
export function previewRecord(input: RecordInput): Promise<PreviewResult> {
  return request<PreviewResult>('/records/preview', { method: 'POST', body: input });
}

/** Preview an edit to an existing record, diffed against the currently committed version. */
export function previewRecordEdit(id: string, input: RecordInput): Promise<PreviewResult> {
  return request<PreviewResult>(`/records/${encodeURIComponent(id)}/preview`, {
    method: 'PUT',
    body: input,
  });
}

export function createRecord(input: RecordInput): Promise<TfRecord> {
  return request<TfRecord>('/records', { method: 'POST', body: input });
}

/**
 * ifMatch is the record's last-seen updated_at/version, sent as the If-Match
 * header for optimistic concurrency (409 on stale writes per openapi.yaml).
 */
export function updateRecord(id: string, input: RecordInput, ifMatch: string): Promise<TfRecord> {
  return request<TfRecord>(`/records/${encodeURIComponent(id)}`, {
    method: 'PUT',
    body: input,
    headers: { 'If-Match': ifMatch },
  });
}

/** Soft-delete only; output repo files are never auto-removed (locked decision). */
export function deleteRecord(id: string): Promise<void> {
  return request<void>(`/records/${encodeURIComponent(id)}`, { method: 'DELETE' });
}

export function getSyncStatus(id: string): Promise<SyncStatusResponse> {
  return request<SyncStatusResponse>(`/records/${encodeURIComponent(id)}/sync-status`);
}

export function retrySync(id: string): Promise<void> {
  return request<void>(`/records/${encodeURIComponent(id)}/retry-sync`, { method: 'POST' });
}
