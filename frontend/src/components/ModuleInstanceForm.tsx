import { useMemo, useState } from 'react';
import type { InstanceValues, ModuleSchema, VarSchema } from '../api/types';
import { useRecordDraft } from '../state/recordDraft';

/**
 * Renders the editable form for one selected module's instances: add/remove
 * instances, and per-instance fields driven by the module's VarSchema[].
 *
 * Field UX rules (see plan/ARCHITECTURE.md "Forms / UI" edge cases and the
 * Naming Convention feature description):
 *  - No default (required or optional-nullable) -> always editable.
 *  - Has a default -> rendered locked/read-only showing the default, with an
 *    explicit "Edit" toggle that unlocks it for override. This is called out
 *    repeatedly in the architecture doc, not optional polish.
 *  - The module's naming_target_variable (metadata.naming_target_variable,
 *    falling back to instance_label_field when naming_resource_type is set)
 *    is always editable and pre-filled from the suggestedName computed by
 *    the last preview call, rather than blank or locked.
 *
 * IMPORTANT: validation here is client-side convenience only (fast feedback,
 * fewer round trips). POST/PUT /api/records/preview is the authoritative
 * validator per plan/ARCHITECTURE.md ("server re-validates authoritatively
 * regardless") — this component must never be trusted as the sole gate.
 */

const INSTANCE_NAME_PATTERN = /^[a-z0-9-]{1,63}$/;
const MAX_OBJECT_NESTING_DEPTH = 2; // deeper falls back to raw JSON, per ARCHITECTURE.md

export interface ModuleInstanceFormProps {
  moduleName: string;
}

export interface InstanceValidationError {
  instanceName: string;
  varName?: string;
  message: string;
}

/** Exported so RecordEditor can gate Preview/Save on the same rules without duplicating them. */
export function validateInstances(schema: ModuleSchema, instances: Record<string, InstanceValues>): InstanceValidationError[] {
  const errors: InstanceValidationError[] = [];
  const names = Object.keys(instances);

  if (names.length === 0) {
    errors.push({ instanceName: '', message: `${schema.module_name}: at least one instance is required.` });
  }

  for (const name of names) {
    if (!INSTANCE_NAME_PATTERN.test(name)) {
      errors.push({
        instanceName: name,
        message: `Instance name "${name}" must be lowercase alphanumeric + hyphens, up to 63 characters.`,
      });
    }
  }

  for (const [instanceName, values] of Object.entries(instances)) {
    for (const v of schema.variables) {
      if (v.required) {
        const val = values[v.name];
        if (val === undefined || val === null || val === '') {
          errors.push({ instanceName, varName: v.name, message: `"${v.name}" is required.` });
        }
      }
    }
  }

  return errors;
}

function resolveNamingTargetVariable(schema: ModuleSchema): string | null {
  const meta = schema.metadata;
  if (!meta) return null;
  if (meta.naming_target_variable) return meta.naming_target_variable;
  if (meta.naming_resource_type && meta.instance_label_field) return meta.instance_label_field;
  return null;
}

export default function ModuleInstanceForm({ moduleName }: ModuleInstanceFormProps) {
  const { draft, addInstance, removeInstance, renameInstance, updateInstanceField } = useRecordDraft();
  const moduleDraft = draft.modules.find((m) => m.module_name === moduleName);
  const [newInstanceName, setNewInstanceName] = useState('');
  const [addError, setAddError] = useState<string | null>(null);
  const [renameDrafts, setRenameDrafts] = useState<Record<string, string>>({});
  const [renameErrors, setRenameErrors] = useState<Record<string, string>>({});
  const [unlocked, setUnlocked] = useState<Record<string, Set<string>>>({});

  const errors = useMemo(
    () => (moduleDraft ? validateInstances(moduleDraft.schema, moduleDraft.instances) : []),
    [moduleDraft],
  );

  if (!moduleDraft) {
    return <p>Module "{moduleName}" is not selected.</p>;
  }

  const { schema, instances } = moduleDraft;
  const namingTargetVariable = resolveNamingTargetVariable(schema);
  const instanceNames = Object.keys(instances);

  function fieldErrorsFor(instanceName: string, varName: string): string[] {
    return errors.filter((e) => e.instanceName === instanceName && e.varName === varName).map((e) => e.message);
  }

  function isFieldUnlocked(instanceName: string, varName: string): boolean {
    return unlocked[instanceName]?.has(varName) ?? false;
  }

  function toggleUnlock(instanceName: string, varName: string, currentDefault: unknown) {
    setUnlocked((prev) => {
      const next = { ...prev };
      const set = new Set(next[instanceName] ?? []);
      set.add(varName);
      next[instanceName] = set;
      return next;
    });
    // Seed the override with the default so the user edits from a known value.
    if (instances[instanceName]?.[varName] === undefined) {
      updateInstanceField(moduleName, instanceName, varName, currentDefault);
    }
  }

  function handleAddInstance() {
    const name = newInstanceName.trim();
    if (!name) {
      setAddError('Instance name is required.');
      return;
    }
    if (!INSTANCE_NAME_PATTERN.test(name)) {
      setAddError('Use lowercase alphanumeric characters and hyphens only, up to 63 characters.');
      return;
    }
    if (instanceNames.includes(name)) {
      setAddError(`An instance named "${name}" already exists for this module.`);
      return;
    }
    setAddError(null);
    addInstance(moduleName, name);
    setNewInstanceName('');
  }

  function handleRenameCommit(oldName: string) {
    const proposed = (renameDrafts[oldName] ?? oldName).trim();
    if (proposed === oldName) return;
    if (!INSTANCE_NAME_PATTERN.test(proposed)) {
      setRenameErrors((prev) => ({ ...prev, [oldName]: 'Lowercase alphanumeric + hyphens only, up to 63 chars.' }));
      return;
    }
    if (instanceNames.includes(proposed)) {
      setRenameErrors((prev) => ({ ...prev, [oldName]: `"${proposed}" is already used in this module.` }));
      return;
    }
    setRenameErrors((prev) => {
      const next = { ...prev };
      delete next[oldName];
      return next;
    });
    renameInstance(moduleName, oldName, proposed);
  }

  return (
    <div className="module-instance-form">
      <h3>{schema.metadata?.display_name ?? schema.module_name}</h3>
      <p className="field-help">{schema.metadata?.description}</p>

      {instanceNames.length === 0 && <p className="field-error">At least one instance is required for this module.</p>}

      {instanceNames.map((instanceName) => (
        <div className="instance-card" key={instanceName}>
          <div className="instance-card-header">
            <div>
              <label>Instance name</label>{' '}
              <input
                value={renameDrafts[instanceName] ?? instanceName}
                onChange={(e) => setRenameDrafts((prev) => ({ ...prev, [instanceName]: e.target.value }))}
                onBlur={() => handleRenameCommit(instanceName)}
              />
              {renameErrors[instanceName] && <div className="field-error">{renameErrors[instanceName]}</div>}
            </div>
            <button type="button" onClick={() => removeInstance(moduleName, instanceName)}>
              Remove instance
            </button>
          </div>

          {schema.variables.map((v) => (
            <VariableField
              key={v.name}
              variable={v}
              path={v.name}
              depth={0}
              isNamingTarget={v.name === namingTargetVariable}
              suggestedName={draft.suggestedNames[`${moduleName}.${instanceName}`]}
              locked={v.has_default && v.name !== namingTargetVariable && !isFieldUnlocked(instanceName, v.name)}
              onUnlock={() => toggleUnlock(instanceName, v.name, v.default)}
              value={instances[instanceName]?.[v.name]}
              errorMessages={fieldErrorsFor(instanceName, v.name)}
              onChange={(value) => updateInstanceField(moduleName, instanceName, v.name, value)}
            />
          ))}
        </div>
      ))}

      <div className="field-row">
        <label>Add instance</label>
        <div style={{ display: 'flex', gap: 8 }}>
          <input
            placeholder="e.g. web-01"
            value={newInstanceName}
            onChange={(e) => setNewInstanceName(e.target.value)}
          />
          <button type="button" onClick={handleAddInstance}>
            Add
          </button>
        </div>
        {addError && <div className="field-error">{addError}</div>}
      </div>
    </div>
  );
}

interface VariableFieldProps {
  variable: VarSchema;
  path: string;
  depth: number;
  isNamingTarget: boolean;
  suggestedName?: string;
  locked: boolean;
  onUnlock: () => void;
  value: unknown;
  errorMessages: string[];
  onChange: (value: unknown) => void;
}

function VariableField({
  variable,
  isNamingTarget,
  suggestedName,
  locked,
  onUnlock,
  value,
  errorMessages,
  onChange,
}: VariableFieldProps) {
  const displayValue = isNamingTarget && (value === undefined || value === '')
    ? suggestedName ?? variable.default ?? ''
    : value ?? (locked ? variable.default : '');

  return (
    <div className="field-row">
      <label>
        {variable.name}
        {variable.required && ' *'}
        {variable.sensitive && ' ⚠ sensitive'}
      </label>
      {variable.description && <div className="field-help">{variable.description}</div>}
      {isNamingTarget && (
        <div className="field-help">Suggested by the naming convention engine — editable, not locked.</div>
      )}
      {variable.validations.length > 0 && (
        <div className="field-help">
          {variable.validations.map((val, i) => (
            <div key={i}>{val.error_message}</div>
          ))}
        </div>
      )}

      <FieldInput variable={variable} locked={locked} value={displayValue} onChange={onChange} />

      {locked && (
        <button type="button" onClick={onUnlock} style={{ width: 'fit-content' }}>
          Edit
        </button>
      )}
      {errorMessages.map((msg, i) => (
        <div className="field-error" key={i}>
          {msg}
        </div>
      ))}
    </div>
  );
}

function FieldInput({
  variable,
  locked,
  value,
  onChange,
  depth = 0,
}: {
  variable: VarSchema;
  locked: boolean;
  value: unknown;
  onChange: (value: unknown) => void;
  depth?: number;
}) {
  // Computed unconditionally (before any early returns below) so this hook
  // call order stays stable across renders per React's rules of hooks; it's
  // only actually used by the raw-JSON fallback branch at the bottom.
  const jsonText = useMemo(() => {
    try {
      return typeof value === 'string'
        ? value
        : JSON.stringify(value ?? (variable.has_default ? variable.default : {}), null, 2);
    } catch {
      return '';
    }
  }, [value, variable]);

  // Object types: render one level of sub-fields (capped nesting per
  // ARCHITECTURE.md); anything deeper, or a list/set/map/tuple/any/
  // unsupported type, falls back to a raw-JSON textarea.
  if (variable.type === 'object' && variable.object_fields && depth < MAX_OBJECT_NESTING_DEPTH) {
    const objValue = (value as Record<string, unknown>) ?? {};
    return (
      <div className="locked-field" style={{ flexDirection: 'column', alignItems: 'stretch', paddingLeft: 12 }}>
        {Object.entries(variable.object_fields).map(([fieldName, fieldSchema]) => (
          <div key={fieldName} style={{ marginBottom: 8 }}>
            <label style={{ fontWeight: 400, fontSize: '0.85rem' }}>{fieldName}</label>
            <FieldInput
              variable={fieldSchema}
              locked={locked}
              value={objValue[fieldName]}
              onChange={(v) => onChange({ ...objValue, [fieldName]: v })}
              depth={depth + 1}
            />
          </div>
        ))}
      </div>
    );
  }

  if (variable.type === 'bool') {
    return (
      <div className="locked-field">
        <input
          type="checkbox"
          checked={Boolean(value)}
          disabled={locked}
          onChange={(e) => onChange(e.target.checked)}
        />
      </div>
    );
  }

  if (variable.type === 'number') {
    return (
      <div className="locked-field">
        <input
          type="number"
          value={(value as number | string) ?? ''}
          disabled={locked}
          readOnly={locked}
          onChange={(e) => onChange(e.target.value === '' ? undefined : Number(e.target.value))}
        />
      </div>
    );
  }

  if (variable.type === 'string' || variable.type === 'any') {
    return (
      <div className="locked-field">
        <input
          type={variable.sensitive ? 'password' : 'text'}
          value={(value as string) ?? ''}
          disabled={locked}
          readOnly={locked}
          onChange={(e) => onChange(e.target.value)}
        />
      </div>
    );
  }

  // list / set / map / tuple / unsupported: raw JSON fallback (Phase 1 explicitly
  // defers structured nested-list/map editing; see ARCHITECTURE.md edge cases).
  return (
    <div className="locked-field" style={{ flexDirection: 'column', alignItems: 'stretch' }}>
      <textarea
        rows={4}
        value={jsonText}
        disabled={locked}
        readOnly={locked}
        onChange={(e) => {
          const text = e.target.value;
          try {
            onChange(JSON.parse(text));
          } catch {
            // Keep raw text until it's valid JSON again; server-side preview
            // will surface a clear error if it's still invalid on submit.
            onChange(text);
          }
        }}
      />
      <div className="field-help">Raw JSON for {variable.type_raw}</div>
    </div>
  );
}
