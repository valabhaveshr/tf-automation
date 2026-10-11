import { beforeAll, describe, expect, it } from 'vitest';
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { MantineProvider } from '@mantine/core';
import { ModalsProvider } from '@mantine/modals';
import { RecordDraftProvider } from '../state/recordDraft';
import { theme } from '../theme';
import type { ModuleSchema } from '../api/types';
import ModulePane from './ModulePane';
import computeInstance from '../test/fixtures/compute-instance.schema.json';
import projectIam from '../test/fixtures/iam-project-iam.schema.json';

/**
 * Render smoke test for the module pane against the real schemas the backend
 * returns for the C:\repos\tf-modules modules (fixtures captured from
 * GET /api/catalog/<module>/schema). Catches render-time crashes and checks
 * the metadata.yaml-driven layout: section titles, the i/e hint markers,
 * defaults, and dropdowns detected from validations.
 */
beforeAll(() => {
  (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
  // jsdom lacks these browser APIs, which Mantine uses.
  window.matchMedia ??= ((query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  })) as typeof window.matchMedia;
  globalThis.ResizeObserver ??= class {
    observe() {}
    unobserve() {}
    disconnect() {}
  } as unknown as typeof ResizeObserver;
});

async function render(schema: ModuleSchema, instances: Record<string, Record<string, unknown>>) {
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);
  await act(async () => {
    root.render(
      <MantineProvider theme={theme}>
        <ModalsProvider>
          <RecordDraftProvider>
            <ModulePane
              module={{ module_name: schema.module_name, pinned_version: schema.version_tag, schema, instances }}
              accent="teal"
              showErrors
            />
          </RecordDraftProvider>
        </ModalsProvider>
      </MantineProvider>,
    );
  });
  const html = container.innerHTML;
  act(() => root.unmount());
  container.remove();
  return html;
}

describe('ModulePane', () => {
  it('renders compute/instance with grouped sections and hints', async () => {
    const html = await render(computeInstance as ModuleSchema, { 'instance-1': { name: 'web-01' } });
    expect(html).toContain('Compute Instance');
    for (const section of ['Basics', 'Networking', 'Boot disk', 'Security']) {
      expect(html).toContain(section);
    }
    expect(html).toContain('Advanced settings');
    expect(html).toContain('data-kind="info"');
    expect(html).toContain('data-kind="example"');
    // Required fields left blank are flagged.
    expect(html).toContain('Machine type is required.');
    // Variables with a default carry the "default" marker.
    expect(html).toContain('>default<');
  });

  it('renders iam/project-iam', async () => {
    const html = await render(projectIam as ModuleSchema, {
      'binding-1': { project_id: 'acme-prod-01', member: 'group:ops@acme.com', role: 'roles/viewer' },
    });
    expect(html).toContain('binding-1');
    expect(html).not.toContain('is required.');
  });

  it('shows an empty state when the module has no instances', async () => {
    const html = await render(projectIam as ModuleSchema, {});
    expect(html).toContain('No instances yet');
  });
});
