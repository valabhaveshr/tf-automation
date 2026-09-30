import { FormEvent, useCallback, useEffect, useState } from 'react';
import * as api from '../api/client';
import { isAdmin, type AuditLogEntry, type AuditLogResourceType } from '../api/types';
import { useAuth } from '../state/auth';

const RESOURCE_TYPES: AuditLogResourceType[] = ['record', 'catalog', 'naming_template', 'customer'];
const PAGE_SIZE = 50;

/**
 * Admin-only audit log viewer (GET /admin/audit-log). Role-gated the same
 * way as CatalogBrowser's refresh button (isAdmin(user)); App.tsx also keeps
 * this route out of non-admins' reach entirely (RequireAdmin), but this
 * component re-checks so it renders nothing useful even if ever mounted
 * directly (e.g. during future refactors of the route guard).
 */
export default function AuditLogPage() {
  const { user } = useAuth();
  const [entries, setEntries] = useState<AuditLogEntry[]>([]);
  const [resourceType, setResourceType] = useState<AuditLogResourceType | ''>('');
  const [actor, setActor] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [hasMore, setHasMore] = useState(true);

  const load = useCallback(
    async (opts: { reset: boolean }) => {
      setLoading(true);
      setError(null);
      try {
        const before = opts.reset ? undefined : entries[entries.length - 1]?.created_at;
        const data = await api.getAuditLog({
          resource_type: resourceType || undefined,
          actor: actor.trim() || undefined,
          limit: PAGE_SIZE,
          before,
        });
        setEntries((prev) => (opts.reset ? data : [...prev, ...data]));
        setHasMore(data.length === PAGE_SIZE);
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to load audit log.');
      } finally {
        setLoading(false);
      }
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [resourceType, actor],
  );

  useEffect(() => {
    if (!isAdmin(user)) return;
    load({ reset: true });
    // Filters changed -> reset. Deliberately not depending on `entries`/`load`.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [user, resourceType, actor]);

  if (!isAdmin(user)) {
    // Defense in depth: App.tsx's route guard should already keep non-admins
    // from reaching this page.
    return <p className="field-error">You do not have access to this page.</p>;
  }

  function handleFilterSubmit(e: FormEvent) {
    e.preventDefault();
    load({ reset: true });
  }

  return (
    <div className="audit-log-page">
      <h2>Audit log</h2>

      <form onSubmit={handleFilterSubmit} style={{ display: 'flex', gap: 12, alignItems: 'flex-end', flexWrap: 'wrap' }}>
        <div className="field-row">
          <label>Resource type</label>
          <select value={resourceType} onChange={(e) => setResourceType(e.target.value as AuditLogResourceType | '')}>
            <option value="">All</option>
            {RESOURCE_TYPES.map((rt) => (
              <option key={rt} value={rt}>
                {rt}
              </option>
            ))}
          </select>
        </div>
        <div className="field-row">
          <label>Actor</label>
          <input
            placeholder="username"
            value={actor}
            onChange={(e) => setActor(e.target.value)}
          />
        </div>
        <button type="submit" disabled={loading}>
          {loading ? 'Loading...' : 'Filter'}
        </button>
      </form>

      {error && <p className="field-error">{error}</p>}

      {entries.length === 0 && !loading && <p>No audit log entries match your filters.</p>}

      {entries.length > 0 && (
        <table>
          <thead>
            <tr>
              <th>When</th>
              <th>Actor</th>
              <th>Action</th>
              <th>Resource</th>
            </tr>
          </thead>
          <tbody>
            {entries.map((entry) => (
              <tr key={entry.id}>
                <td>{new Date(entry.created_at).toLocaleString()}</td>
                <td>{entry.actor_username}</td>
                <td>{entry.action}</td>
                <td>
                  {entry.resource_type}
                  {entry.resource_id ? ` (${entry.resource_id})` : ''}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      {hasMore && entries.length > 0 && (
        <button type="button" onClick={() => load({ reset: false })} disabled={loading}>
          {loading ? 'Loading...' : 'Load more'}
        </button>
      )}
    </div>
  );
}
