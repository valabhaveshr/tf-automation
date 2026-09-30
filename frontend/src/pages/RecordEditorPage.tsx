import { useEffect, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import * as api from '../api/client';
import { ApiError } from '../api/client';
import type { PreviewResult } from '../api/types';
import { useRecordDraft } from '../state/recordDraft';
import CatalogBrowser from '../components/CatalogBrowser';
import ModuleInstanceForm, { validateInstances } from '../components/ModuleInstanceForm';
import PreviewDiff from '../components/PreviewDiff';

/**
 * The main authoring flow: CatalogBrowser -> one ModuleInstanceForm per
 * selected module -> customer/project/env fields -> Preview (via
 * PreviewDiff) -> Save. Handles both "new record" (no :id) and "edit
 * existing record" (:id present) modes against the same draft store.
 */
export default function RecordEditorPage() {
  const { id } = useParams<{ id: string }>();
  const isEditing = Boolean(id);
  const navigate = useNavigate();
  const { draft, setCustomerFields, removeModule, loadDraft, setSuggestedNames, toRecordInput, reset } =
    useRecordDraft();

  const [lastUpdatedAt, setLastUpdatedAt] = useState<string | null>(null);
  const [loadingRecord, setLoadingRecord] = useState(isEditing);
  const [previewResult, setPreviewResult] = useState<PreviewResult | null>(null);
  const [previewing, setPreviewing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);

  // Load an existing record into the draft when editing; start clean for a new record.
  useEffect(() => {
    if (!isEditing) {
      reset();
      return;
    }
    if (!id) return;
    setLoadingRecord(true);
    (async () => {
      try {
        const record = await api.getRecord(id);
        setLastUpdatedAt(record.updated_at);
        const modules = await Promise.all(
          record.module_selections.map(async (sel) => {
            const schema = await api.getModuleSchema(sel.module_name, sel.pinned_version ?? undefined);
            return {
              module_name: sel.module_name,
              pinned_version: sel.pinned_version,
              schema,
              instances: sel.instances,
            };
          }),
        );
        loadDraft({
          recordId: record.id,
          customer_name: record.customer_name,
          project_name: record.project_name,
          env_name: record.env_name,
          modules,
          suggestedNames: {},
        });
      } catch (err) {
        setFormError(err instanceof Error ? err.message : 'Failed to load record for editing.');
      } finally {
        setLoadingRecord(false);
      }
    })();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id, isEditing]);

  const validationErrors = draft.modules.flatMap((m) => validateInstances(m.schema, m.instances));
  const hasSensitiveVariables = draft.modules.some((m) => m.schema.variables.some((v) => v.sensitive));

  function guardBasicFields(): string | null {
    if (!draft.customer_name.trim()) return 'Customer name is required.';
    if (!draft.project_name.trim()) return 'Project name is required.';
    if (!draft.env_name.trim()) return 'Environment name is required.';
    if (draft.modules.length === 0) return 'Select at least one module.';
    return null;
  }

  async function handlePreview() {
    setFormError(null);
    const basicError = guardBasicFields();
    if (basicError) {
      setFormError(basicError);
      return;
    }
    if (validationErrors.length > 0) {
      setFormError('Fix the highlighted field errors before previewing.');
      return;
    }
    setPreviewing(true);
    try {
      const input = toRecordInput();
      const result = isEditing && id ? await api.previewRecordEdit(id, input) : await api.previewRecord(input);
      setPreviewResult(result);
      setSuggestedNames(result.suggested_names ?? {});
    } catch (err) {
      if (err instanceof ApiError) {
        setFormError(`Preview failed: ${err.message}`);
      } else {
        setFormError('Preview failed.');
      }
    } finally {
      setPreviewing(false);
    }
  }

  async function handleSave() {
    setFormError(null);
    const basicError = guardBasicFields();
    if (basicError) {
      setFormError(basicError);
      return;
    }
    if (validationErrors.length > 0) {
      setFormError('Fix the highlighted field errors before saving.');
      return;
    }
    setSaving(true);
    try {
      const input = toRecordInput();
      const saved =
        isEditing && id && lastUpdatedAt
          ? await api.updateRecord(id, input, lastUpdatedAt)
          : await api.createRecord(input);
      navigate(`/records/${saved.id}`);
    } catch (err) {
      if (err instanceof ApiError && err.status === 409) {
        setFormError(
          isEditing
            ? 'This record was updated elsewhere since you loaded it. Reload and re-apply your changes.'
            : 'A record for this customer/project/env already exists.',
        );
      } else if (err instanceof ApiError) {
        setFormError(`Save failed: ${err.message}`);
      } else {
        setFormError('Save failed.');
      }
    } finally {
      setSaving(false);
    }
  }

  if (loadingRecord) return <p>Loading record...</p>;

  return (
    <div>
      <h2>{isEditing ? 'Edit record' : 'New record'}</h2>

      <div className="field-row">
        <label>Customer name</label>
        <input
          value={draft.customer_name}
          onChange={(e) => setCustomerFields({ customer_name: e.target.value })}
        />
      </div>
      <div className="field-row">
        <label>Project name</label>
        <input value={draft.project_name} onChange={(e) => setCustomerFields({ project_name: e.target.value })} />
      </div>
      <div className="field-row">
        <label>Environment name</label>
        <input value={draft.env_name} onChange={(e) => setCustomerFields({ env_name: e.target.value })} />
      </div>

      <h3>Modules</h3>
      <CatalogBrowser />

      {draft.modules.map((m) => (
        <div key={m.module_name}>
          <ModuleInstanceForm moduleName={m.module_name} />
          <button type="button" onClick={() => removeModule(m.module_name)}>
            Remove {m.module_name} from record
          </button>
        </div>
      ))}

      {formError && <p className="field-error">{formError}</p>}

      <div style={{ display: 'flex', gap: 12, margin: '16px 0' }}>
        <button type="button" onClick={handlePreview} disabled={previewing}>
          {previewing ? 'Generating preview...' : 'Preview'}
        </button>
        <button type="button" onClick={handleSave} disabled={saving}>
          {saving ? 'Saving...' : 'Save'}
        </button>
      </div>

      {/* Client-side validation above is convenience only; POST/PUT
          /api/records/preview is the authoritative validator per
          plan/ARCHITECTURE.md, and the actual preview call happens on
          "Preview" / "Save" regardless of what this component thinks. */}

      {previewResult && <PreviewDiff result={previewResult} hasSensitiveVariables={hasSensitiveVariables} />}
    </div>
  );
}
