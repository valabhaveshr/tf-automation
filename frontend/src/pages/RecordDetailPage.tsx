import { useEffect, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import * as api from '../api/client';
import type { TfRecord } from '../api/types';

export default function RecordDetailPage() {
  const { id } = useParams<{ id: string }>();
  const [record, setRecord] = useState<TfRecord | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [retrying, setRetrying] = useState(false);

  async function load() {
    if (!id) return;
    setLoading(true);
    setError(null);
    try {
      const data = await api.getRecord(id);
      setRecord(data);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load record.');
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id]);

  async function handleRetrySync() {
    if (!id) return;
    setRetrying(true);
    setError(null);
    try {
      await api.retrySync(id);
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Retry failed.');
    } finally {
      setRetrying(false);
    }
  }

  if (loading) return <p>Loading...</p>;
  if (error) return <p className="field-error">{error}</p>;
  if (!record) return <p>Record not found.</p>;

  return (
    <div>
      <h2>
        {record.customer_name} / {record.project_name} / {record.env_name}
      </h2>
      <p>Output path: {record.output_path}</p>
      <p>
        Sync status: <strong>{record.sync_status}</strong>
        {record.sync_status === 'failed' && (
          <button type="button" onClick={handleRetrySync} disabled={retrying} style={{ marginLeft: 8 }}>
            {retrying ? 'Retrying...' : 'Retry sync'}
          </button>
        )}
      </p>
      {record.last_commit_sha && <p>Last commit: {record.last_commit_sha}</p>}
      <p>
        Created by {record.created_by} at {new Date(record.created_at).toLocaleString()}
      </p>
      <p>
        Last updated by {record.updated_by} at {new Date(record.updated_at).toLocaleString()}
      </p>

      <h3>Module selections</h3>
      {record.module_selections.map((sel) => (
        <div className="instance-card" key={sel.module_name}>
          <strong>{sel.module_name}</strong> ({sel.pinned_version ?? 'unpinned'})
          <ul>
            {Object.keys(sel.instances).map((instanceName) => (
              <li key={instanceName}>{instanceName}</li>
            ))}
          </ul>
        </div>
      ))}

      <Link to={`/records/${record.id}/edit`}>Edit this record</Link>
    </div>
  );
}
