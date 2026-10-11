/**
 * In-progress record draft store, shared across CatalogBrowser,
 * ModuleInstanceForm, PreviewDiff, and the save step in RecordEditor.
 *
 * Kept deliberately simple (React Context + useState, no Redux) since the
 * draft is single-user, single-screen state that doesn't need time-travel,
 * middleware, or cross-tab sync for a Phase 1 MVP.
 */
import React, { createContext, useCallback, useContext, useMemo, useState } from 'react';
import type { InstancesMap, InstanceValues, ModuleSchema, RecordInput } from '../api/types';

export interface DraftModuleSelection {
  module_name: string;
  /** Set once a preview/save has pinned a specific tag; undefined = "pin latest at submit time". */
  pinned_version?: string | null;
  /** Full schema kept on the draft so ModuleInstanceForm can render without refetching. */
  schema: ModuleSchema;
  instances: InstancesMap;
}

export interface RecordDraftState {
  recordId: string | null;
  /** Platform admins pick the customer; customer users always get their own. */
  customer_id: string;
  customer_name: string;
  project_name: string;
  env_name: string;
  modules: DraftModuleSelection[];
  /** "module_name.instance_name" -> tentative Naming Convention Engine suggestion, from the last preview. */
  suggestedNames: Record<string, string>;
}

const emptyDraft: RecordDraftState = {
  recordId: null,
  customer_id: '',
  customer_name: '',
  project_name: '',
  env_name: '',
  modules: [],
  suggestedNames: {},
};

export interface RecordDraftApi {
  draft: RecordDraftState;
  setCustomerFields: (
    fields: Partial<Pick<RecordDraftState, 'customer_id' | 'customer_name' | 'project_name' | 'env_name'>>,
  ) => void;
  addModule: (schema: ModuleSchema) => void;
  removeModule: (moduleName: string) => void;
  renameInstance: (moduleName: string, oldName: string, newName: string) => void;
  addInstance: (moduleName: string, instanceName: string, initialValues?: InstanceValues) => void;
  removeInstance: (moduleName: string, instanceName: string) => void;
  duplicateInstance: (moduleName: string, sourceName: string, newName: string) => void;
  /** value === undefined removes the field, so the module's default applies again. */
  updateInstanceField: (moduleName: string, instanceName: string, varName: string, value: unknown) => void;
  setSuggestedNames: (suggestions: Record<string, string>) => void;
  loadDraft: (state: RecordDraftState) => void;
  reset: () => void;
  toRecordInput: () => RecordInput;
}

const RecordDraftContext = createContext<RecordDraftApi | undefined>(undefined);

export function RecordDraftProvider({ children }: { children: React.ReactNode }) {
  const [draft, setDraft] = useState<RecordDraftState>(emptyDraft);

  const setCustomerFields = useCallback(
    (fields: Partial<Pick<RecordDraftState, 'customer_id' | 'customer_name' | 'project_name' | 'env_name'>>) => {
      setDraft((prev) => ({ ...prev, ...fields }));
    },
    [],
  );

  const addModule = useCallback((schema: ModuleSchema) => {
    setDraft((prev) => {
      if (prev.modules.some((m) => m.module_name === schema.module_name)) {
        return prev; // already selected; no-op
      }
      return {
        ...prev,
        modules: [
          ...prev.modules,
          {
            module_name: schema.module_name,
            pinned_version: schema.version_tag,
            schema,
            instances: {},
          },
        ],
      };
    });
  }, []);

  const removeModule = useCallback((moduleName: string) => {
    setDraft((prev) => ({
      ...prev,
      modules: prev.modules.filter((m) => m.module_name !== moduleName),
    }));
  }, []);

  const addInstance = useCallback(
    (moduleName: string, instanceName: string, initialValues: InstanceValues = {}) => {
      setDraft((prev) => ({
        ...prev,
        modules: prev.modules.map((m) =>
          m.module_name === moduleName
            ? { ...m, instances: { ...m.instances, [instanceName]: initialValues } }
            : m,
        ),
      }));
    },
    [],
  );

  const removeInstance = useCallback((moduleName: string, instanceName: string) => {
    setDraft((prev) => ({
      ...prev,
      modules: prev.modules.map((m) => {
        if (m.module_name !== moduleName) return m;
        const rest = { ...m.instances };
        delete rest[instanceName];
        return { ...m, instances: rest };
      }),
    }));
  }, []);

  const duplicateInstance = useCallback((moduleName: string, sourceName: string, newName: string) => {
    setDraft((prev) => ({
      ...prev,
      modules: prev.modules.map((m) => {
        if (m.module_name !== moduleName || !(sourceName in m.instances) || newName in m.instances) return m;
        const copy = JSON.parse(JSON.stringify(m.instances[sourceName] ?? {})) as InstanceValues;
        return { ...m, instances: { ...m.instances, [newName]: copy } };
      }),
    }));
  }, []);

  const renameInstance = useCallback((moduleName: string, oldName: string, newName: string) => {
    setDraft((prev) => ({
      ...prev,
      modules: prev.modules.map((m) => {
        if (m.module_name !== moduleName) return m;
        if (!(oldName in m.instances) || oldName === newName) return m;
        const rest = { ...m.instances };
        const values = rest[oldName];
        delete rest[oldName];
        rest[newName] = values;
        return { ...m, instances: rest };
      }),
    }));
  }, []);

  const updateInstanceField = useCallback(
    (moduleName: string, instanceName: string, varName: string, value: unknown) => {
      setDraft((prev) => ({
        ...prev,
        modules: prev.modules.map((m) => {
          if (m.module_name !== moduleName) return m;
          const nextInstance = { ...(m.instances[instanceName] ?? {}) };
          if (value === undefined) {
            delete nextInstance[varName];
          } else {
            nextInstance[varName] = value;
          }
          return { ...m, instances: { ...m.instances, [instanceName]: nextInstance } };
        }),
      }));
    },
    [],
  );

  const setSuggestedNames = useCallback((suggestions: Record<string, string>) => {
    setDraft((prev) => ({ ...prev, suggestedNames: { ...prev.suggestedNames, ...suggestions } }));
  }, []);

  const loadDraft = useCallback((state: RecordDraftState) => {
    setDraft(state);
  }, []);

  const reset = useCallback(() => setDraft(emptyDraft), []);

  const toRecordInput = useCallback((): RecordInput => {
    return {
      customer_id: draft.customer_id || undefined,
      customer_name: draft.customer_name,
      project_name: draft.project_name,
      env_name: draft.env_name,
      module_selections: draft.modules.map((m) => ({
        module_name: m.module_name,
        pinned_version: m.pinned_version ?? undefined,
        instances: m.instances,
      })),
    };
  }, [draft]);

  const api = useMemo<RecordDraftApi>(
    () => ({
      draft,
      setCustomerFields,
      addModule,
      removeModule,
      renameInstance,
      addInstance,
      removeInstance,
      duplicateInstance,
      updateInstanceField,
      setSuggestedNames,
      loadDraft,
      reset,
      toRecordInput,
    }),
    [
      draft,
      setCustomerFields,
      addModule,
      removeModule,
      renameInstance,
      addInstance,
      removeInstance,
      duplicateInstance,
      updateInstanceField,
      setSuggestedNames,
      loadDraft,
      reset,
      toRecordInput,
    ],
  );

  return React.createElement(RecordDraftContext.Provider, { value: api }, children);
}

export function useRecordDraft(): RecordDraftApi {
  const ctx = useContext(RecordDraftContext);
  if (!ctx) {
    throw new Error('useRecordDraft must be used within a RecordDraftProvider');
  }
  return ctx;
}
