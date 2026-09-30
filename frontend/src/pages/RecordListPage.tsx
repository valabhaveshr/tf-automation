import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import * as api from '../api/client';
import type { RecordSummary } from '../api/types';

export default function RecordListPage() {
  const [records, setRecords] = useState<RecordSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    api
      .listRecords({ active: true })
      .then(setRecords)
      .catch((err) => setError(err instanceof Error ? err.message : 'Failed to load records.'))
      .finally(() => setLoading(false));
  }, []);

  return (
    <div>
      <h2>Records</h2>
      {loading && <p>Loading...</p>}
      {error && <p className="field-error">{error}</p>}
      {!loading && records.length === 0 && <p>No records yet. Create one to get started.</p>}
      {records.length > 0 && (
        <table>
          <thead>
            <tr>
              <th>Customer</th>
              <th>Project</th>
              <th>Env</th>
              <th>Sync status</th>
              <th>Updated</th>
            </tr>
          </thead>
          <tbody>
            {records.map((r) => (
              <tr key={r.id}>
                <td>
                  <Link to={`/records/${r.id}`}>{r.customer_name}</Link>
                </td>
                <td>{r.project_name}</td>
                <td>{r.env_name}</td>
                <td>{r.sync_status}</td>
                <td>{new Date(r.updated_at).toLocaleString()}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}
