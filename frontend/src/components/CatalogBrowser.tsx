import { useEffect, useMemo, useState } from 'react';
import * as api from '../api/client';
import { isAdmin, type CatalogEntry } from '../api/types';
import { useAuth } from '../state/auth';
import { useRecordDraft } from '../state/recordDraft';

/**
 * Lists/searches the module catalog (GET /catalog), grouped by category, and
 * lets the user add one or more modules to the in-progress record draft.
 * Selecting a module fetches its full schema (GET /catalog/:name/schema) so
 * ModuleInstanceForm has everything it needs without a further round trip.
 */
export default function CatalogBrowser() {
  const { user } = useAuth();
  const { draft, addModule } = useRecordDraft();
  const [entries, setEntries] = useState<CatalogEntry[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [search, setSearch] = useState('');
  const [refreshing, setRefreshing] = useState(false);

  async function loadCatalog() {
    setLoading(true);
    setError(null);
    try {
      const data = await api.getCatalog();
      setEntries(data);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load catalog.');
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    loadCatalog();
  }, []);

  const selectedNames = useMemo(() => new Set(draft.modules.map((m) => m.module_name)), [draft.modules]);

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    if (!q) return entries;
    return entries.filter(
      (e) =>
        e.module_name.toLowerCase().includes(q) ||
        e.display_name.toLowerCase().includes(q) ||
        e.description?.toLowerCase().includes(q) ||
        e.category.toLowerCase().includes(q),
    );
  }, [entries, search]);

  const grouped = useMemo(() => {
    const byCategory = new Map<string, CatalogEntry[]>();
    for (const entry of filtered) {
      const list = byCategory.get(entry.category) ?? [];
      list.push(entry);
      byCategory.set(entry.category, list);
    }
    return byCategory;
  }, [filtered]);

  async function handleSelect(entry: CatalogEntry) {
    if (selectedNames.has(entry.module_name)) return;
    try {
      const schema = await api.getModuleSchema(entry.module_name);
      addModule(schema);
    } catch (err) {
      setError(err instanceof Error ? err.message : `Failed to load schema for ${entry.module_name}.`);
    }
  }

  async function handleRefreshCatalog() {
    setRefreshing(true);
    setError(null);
    try {
      await api.refreshCatalog();
      await loadCatalog();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Catalog refresh failed.');
    } finally {
      setRefreshing(false);
    }
  }

  return (
    <div className="catalog-browser">
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: 12 }}>
        <input
          placeholder="Search modules..."
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          style={{ flex: 1, maxWidth: 320 }}
        />
        {/* Admin-only manual catalog refresh (roles.includes('admin')). Not shown to normal users. */}
        {isAdmin(user) && (
          <button type="button" onClick={handleRefreshCatalog} disabled={refreshing}>
            {refreshing ? 'Refreshing...' : 'Refresh catalog'}
          </button>
        )}
      </div>

      {loading && <p>Loading catalog...</p>}
      {error && <p className="field-error">{error}</p>}

      {[...grouped.entries()].map(([category, categoryEntries]) => (
        <div className="category-group" key={category}>
          <h3>{category}</h3>
          {categoryEntries.map((entry) => {
            const isSelected = selectedNames.has(entry.module_name);
            return (
              <div className="module-card" key={entry.module_name}>
                <div>
                  <strong>{entry.display_name}</strong> ({entry.cloud})
                  <div className="field-help">{entry.description}</div>
                  <div className="field-help">Version: {entry.latest_version_tag}</div>
                  {entry.naming_resource_type && (
                    <div className="field-help">Naming resource type: {entry.naming_resource_type}</div>
                  )}
                </div>
                <button type="button" disabled={isSelected} onClick={() => handleSelect(entry)}>
                  {isSelected ? 'Added' : 'Add to record'}
                </button>
              </div>
            );
          })}
        </div>
      ))}

      {!loading && filtered.length === 0 && <p>No modules match your search.</p>}
    </div>
  );
}
