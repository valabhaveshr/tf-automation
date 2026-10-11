import { useEffect, useMemo, useState, type ReactNode } from 'react';
import {
  Alert,
  Autocomplete,
  Badge,
  Box,
  Button,
  Card,
  Divider,
  Grid,
  Group,
  List,
  Loader,
  Select,
  Stack,
  Text,
  TextInput,
  ThemeIcon,
  Title,
} from '@mantine/core';
import { useWindowEvent } from '@mantine/hooks';
import { notifications } from '@mantine/notifications';
import {
  IconAlertCircle,
  IconAlertTriangle,
  IconCode,
  IconDeviceFloppy,
  IconFolder,
  IconPackages,
  IconPuzzle,
} from '@tabler/icons-react';
import { useNavigate, useParams } from 'react-router-dom';
import * as api from '../api/client';
import { ApiError } from '../api/client';
import type { CatalogEntry, Customer, PreviewResult } from '../api/types';
import { useAuth } from '../state/auth';
import { useRecordDraft } from '../state/recordDraft';
import { nextInstanceName, validateInstances } from '../lib/schemaForm';
import { envColor, errorMessage, outputPath, shortCode } from '../lib/format';
import { MODULE_ACCENTS } from '../theme';
import ModulePicker from '../components/ModulePicker';
import ModulePane from '../components/ModulePane';
import PreviewDrawer from '../components/PreviewDrawer';
import { EmptyState, PageHeader } from '../components/common';

const ENVIRONMENTS = ['dev', 'test', 'qa', 'staging', 'uat', 'prod'];

/**
 * New resource request (/resources/new) or edit (/resources/:id/edit):
 * request details -> modules (each in its own pane, several instances
 * each) -> preview the generated Terraform -> save.
 */
export default function ResourceEditorPage() {
  const { id } = useParams<{ id: string }>();
  const isEditing = Boolean(id);
  const navigate = useNavigate();
  const { me } = useAuth();
  const platform = !!me?.is_platform_admin;
  const { draft, setCustomerFields, addModule, addInstance, loadDraft, setSuggestedNames, toRecordInput, reset } = useRecordDraft();

  const [catalog, setCatalog] = useState<CatalogEntry[]>([]);
  const [catalogLoading, setCatalogLoading] = useState(true);
  const [customers, setCustomers] = useState<Customer[]>([]);
  const [lastUpdatedAt, setLastUpdatedAt] = useState<string | null>(null);
  const [loadingRecord, setLoadingRecord] = useState(isEditing);
  const [addingModule, setAddingModule] = useState<string | null>(null);
  const [showErrors, setShowErrors] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);
  const [preview, setPreview] = useState<PreviewResult | null>(null);
  const [previewOpen, setPreviewOpen] = useState(false);
  const [previewing, setPreviewing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);

  useEffect(() => {
    api
      .getCatalog()
      .then(setCatalog)
      .catch((err) => setFormError(errorMessage(err, 'Could not load the module catalog.')))
      .finally(() => setCatalogLoading(false));
    if (platform) api.listCustomers().then(setCustomers).catch(() => setCustomers([]));
  }, [platform]);

  // Start clean for a new request; load the record into the draft when editing.
  useEffect(() => {
    setPreview(null);
    setShowErrors(false);
    setSaved(false);
    if (!isEditing || !id) {
      reset();
      if (me?.customer) setCustomerFields({ customer_id: me.customer.id, customer_name: me.customer.name });
      return;
    }
    setLoadingRecord(true);
    (async () => {
      try {
        const record = await api.getRecord(id);
        setLastUpdatedAt(record.updated_at);
        const modules = await Promise.all(
          record.module_selections.map(async (sel) => ({
            module_name: sel.module_name,
            pinned_version: sel.pinned_version,
            schema: await api.getModuleSchema(sel.module_name, sel.pinned_version ?? undefined),
            instances: sel.instances ?? {},
          })),
        );
        loadDraft({
          recordId: record.id,
          customer_id: record.customer_id ?? '',
          customer_name: record.customer_name,
          project_name: record.project_name,
          env_name: record.env_name,
          modules,
          suggestedNames: {},
        });
      } catch (err) {
        setFormError(errorMessage(err, 'Could not load this resource for editing.'));
      } finally {
        setLoadingRecord(false);
      }
    })();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id, isEditing]);

  // Any draft change makes the last preview stale.
  const draftKey = JSON.stringify(toRecordInput());
  useEffect(() => setPreview(null), [draftKey]);

  // Warn before closing or reloading the tab while the form has content.
  const hasContent = draft.modules.length > 0 || !!draft.project_name || !!draft.env_name;
  useWindowEvent('beforeunload', (e) => {
    if (hasContent && !saved) {
      e.preventDefault();
      e.returnValue = '';
    }
  });

  const customerName = platform ? customers.find((c) => c.id === draft.customer_id)?.name ?? draft.customer_name : me?.customer?.name ?? '';
  const path = outputPath(customerName, draft.project_name, draft.env_name);
  const validationErrors = useMemo(
    () => draft.modules.flatMap((m) => validateInstances(m.schema, m.instances)),
    [draft.modules],
  );
  const hasSensitive = draft.modules.some((m) => m.schema.variables.some((v) => v.sensitive));
  const totalInstances = draft.modules.reduce((n, m) => n + Object.keys(m.instances).length, 0);

  const basicErrors = {
    customer: platform && !draft.customer_id ? 'Choose the customer this request is for.' : null,
    project: !draft.project_name.trim() ? 'Project name is required.' : null,
    env: !draft.env_name.trim() ? 'Environment is required.' : null,
    modules: draft.modules.length === 0 ? 'Add at least one module.' : null,
  };
  const blockingCount = Object.values(basicErrors).filter(Boolean).length + validationErrors.length;

  async function pickModule(entry: CatalogEntry) {
    setAddingModule(entry.module_name);
    try {
      const schema = await api.getModuleSchema(entry.module_name);
      addModule(schema);
      addInstance(schema.module_name, nextInstanceName(schema.module_name.split('/').pop() ?? 'instance', []));
      notifications.show({ color: 'brand', message: `${entry.display_name} added. Fill in its settings below.` });
    } catch (err) {
      notifications.show({ color: 'red', title: 'Could not add module', message: errorMessage(err, 'Loading the module failed.') });
    } finally {
      setAddingModule(null);
    }
  }

  function readyToSubmit(): boolean {
    setShowErrors(true);
    setFormError(null);
    if (blockingCount > 0) {
      setFormError(`Fix ${blockingCount} issue${blockingCount === 1 ? '' : 's'} highlighted on the page first.`);
      return false;
    }
    return true;
  }

  async function runPreview() {
    if (!readyToSubmit()) return;
    setPreviewing(true);
    try {
      const input = toRecordInput();
      const result = isEditing && id ? await api.previewRecordEdit(id, input) : await api.previewRecord(input);
      setPreview(result);
      setSuggestedNames(result.suggested_names ?? {});
      setPreviewOpen(true);
    } catch (err) {
      setFormError(`Preview failed: ${errorMessage(err, 'unknown error')}`);
    } finally {
      setPreviewing(false);
    }
  }

  async function save() {
    if (!readyToSubmit()) return;
    setSaving(true);
    try {
      const input = toRecordInput();
      const saved = isEditing && id && lastUpdatedAt ? await api.updateRecord(id, input, lastUpdatedAt) : await api.createRecord(input);
      setSaved(true);
      notifications.show({
        color: saved.sync_status === 'failed' ? 'orange' : 'teal',
        title: isEditing ? 'Changes saved' : 'Resource request created',
        message:
          saved.sync_status === 'failed'
            ? 'Saved, but committing to the output repository failed. You can retry from the resource page.'
            : `Committed to ${saved.output_path}.`,
      });
      navigate(`/resources/${saved.id}`);
    } catch (err) {
      setPreviewOpen(false);
      if (err instanceof ApiError && err.status === 409) {
        setFormError(
          isEditing
            ? 'Someone else changed this resource since you opened it. Reload the page and re-apply your changes.'
            : errorMessage(err, 'A resource for this project and environment already exists.'),
        );
      } else {
        setFormError(`Save failed: ${errorMessage(err, 'unknown error')}`);
      }
    } finally {
      setSaving(false);
    }
  }

  if (loadingRecord) {
    return (
      <Group justify="center" py="xl">
        <Loader />
      </Group>
    );
  }

  return (
    <>
      <PageHeader
        title={isEditing ? `Edit ${draft.project_name} / ${draft.env_name}` : 'New resource'}
        description={
          isEditing
            ? 'Change modules or settings. You will see a diff of the generated code before saving.'
            : 'Choose a project and environment, add the modules you need, then preview and save.'
        }
      />

      <Grid gutter="lg" align="flex-start">
        <Grid.Col span={{ base: 12, lg: 8 }}>
          <Stack gap="lg">
            <Card p="lg">
              <SectionTitle icon={<IconFolder size={18} />} step={1} title="Request details" />
              <Grid mt="md">
                {platform ? (
                  <Grid.Col span={{ base: 12, md: 4 }}>
                    <Select
                      label="Customer"
                      withAsterisk
                      placeholder="Choose a customer"
                      data={customers.map((c) => ({ value: c.id, label: `${c.name} (${shortCode(c.business_short_code)})` }))}
                      value={draft.customer_id || null}
                      onChange={(v) => setCustomerFields({ customer_id: v ?? '', customer_name: customers.find((c) => c.id === v)?.name ?? '' })}
                      searchable
                      disabled={isEditing}
                      error={showErrors && basicErrors.customer}
                    />
                  </Grid.Col>
                ) : (
                  <Grid.Col span={{ base: 12, md: 4 }}>
                    <TextInput label="Customer" value={customerName} readOnly variant="filled" />
                  </Grid.Col>
                )}
                <Grid.Col span={{ base: 12, md: 4 }}>
                  <TextInput
                    label="Project name"
                    withAsterisk
                    placeholder="e.g. booking-platform"
                    value={draft.project_name}
                    onChange={(e) => setCustomerFields({ project_name: e.currentTarget.value })}
                    error={showErrors && basicErrors.project}
                  />
                </Grid.Col>
                <Grid.Col span={{ base: 12, md: 4 }}>
                  <Autocomplete
                    label="Environment"
                    withAsterisk
                    placeholder="e.g. dev, staging, prod"
                    data={ENVIRONMENTS}
                    value={draft.env_name}
                    onChange={(v) => setCustomerFields({ env_name: v })}
                    error={showErrors && basicErrors.env}
                  />
                </Grid.Col>
              </Grid>
              {path && (
                <Text size="xs" c="dimmed" mt="sm">
                  Output folder:{' '}
                  <Text span ff="monospace" size="xs">
                    {path}/
                  </Text>
                </Text>
              )}
            </Card>

            <Card p="lg">
              <SectionTitle icon={<IconPackages size={18} />} step={2} title="Modules" />
              <Box mt="md">
                <ModulePicker
                  catalog={catalog}
                  selected={draft.modules.map((m) => m.module_name)}
                  loading={catalogLoading || !!addingModule}
                  onPick={pickModule}
                />
              </Box>
              {showErrors && basicErrors.modules && (
                <Text c="red" size="sm" mt="xs">
                  {basicErrors.modules}
                </Text>
              )}
            </Card>

            {draft.modules.length === 0 ? (
              <Card p={0} style={{ borderStyle: 'dashed' }}>
                <EmptyState
                  icon={<IconPuzzle size={44} stroke={1.3} />}
                  title="No modules added yet"
                  description="Use the module picker above. Each module gets its own panel here, and you can add several instances of the same module."
                />
              </Card>
            ) : (
              draft.modules.map((m, i) => (
                <ModulePane key={m.module_name} module={m} accent={MODULE_ACCENTS[i % MODULE_ACCENTS.length]} showErrors={showErrors} />
              ))
            )}
          </Stack>
        </Grid.Col>

        <Grid.Col span={{ base: 12, lg: 4 }}>
          <Card p="lg" style={{ position: 'sticky', top: 'calc(var(--app-shell-header-height, 60px) + var(--mantine-spacing-lg))' }}>
            <SectionTitle icon={<IconCode size={18} />} step={3} title="Review & save" />
            <Stack gap="sm" mt="md">
              <SummaryRow label="Customer" value={customerName || '—'} />
              <SummaryRow label="Project" value={draft.project_name || '—'} />
              <SummaryRow
                label="Environment"
                value={
                  draft.env_name ? (
                    <Badge variant="light" color={envColor(draft.env_name)}>
                      {draft.env_name}
                    </Badge>
                  ) : (
                    '—'
                  )
                }
              />
              <Divider />
              <Text size="sm" fw={600}>
                {draft.modules.length} module{draft.modules.length === 1 ? '' : 's'} · {totalInstances} instance
                {totalInstances === 1 ? '' : 's'}
              </Text>
              {draft.modules.map((m, i) => {
                const errs = validateInstances(m.schema, m.instances).length;
                return (
                  <Group key={m.module_name} justify="space-between" wrap="nowrap" gap="xs">
                    <Group gap={8} wrap="nowrap" style={{ minWidth: 0 }}>
                      <Box w={8} h={8} style={{ borderRadius: 4, flexShrink: 0, background: `var(--mantine-color-${MODULE_ACCENTS[i % MODULE_ACCENTS.length]}-6)` }} />
                      <Text size="sm" truncate>
                        {m.schema.metadata?.display_name ?? m.module_name}
                      </Text>
                    </Group>
                    <Group gap={4} wrap="nowrap">
                      <Badge variant="light" color="gray" size="sm">
                        ×{Object.keys(m.instances).length}
                      </Badge>
                      {showErrors && errs > 0 && (
                        <Badge color="red" size="sm">
                          {errs}
                        </Badge>
                      )}
                    </Group>
                  </Group>
                );
              })}

              {hasSensitive && (
                <Alert color="orange" variant="light" icon={<IconAlertTriangle size={16} />} p="xs">
                  <Text size="xs">Sensitive values are written to the generated tfvars in plain text.</Text>
                </Alert>
              )}

              {formError && (
                <Alert color="red" variant="light" icon={<IconAlertCircle size={16} />} p="xs">
                  <Text size="xs">{formError}</Text>
                  {showErrors && validationErrors.length > 0 && (
                    <List size="xs" mt={4}>
                      {validationErrors.slice(0, 5).map((e, i) => (
                        <List.Item key={i}>
                          {e.instanceName && <b>{e.instanceName}: </b>}
                          {e.message}
                        </List.Item>
                      ))}
                      {validationErrors.length > 5 && <List.Item>…and {validationErrors.length - 5} more</List.Item>}
                    </List>
                  )}
                </Alert>
              )}

              <Button variant="default" leftSection={<IconCode size={18} />} onClick={runPreview} loading={previewing} fullWidth>
                Preview generated code
              </Button>
              <Button leftSection={<IconDeviceFloppy size={18} />} onClick={save} loading={saving} fullWidth size="md">
                {isEditing ? 'Save changes' : 'Create resource'}
              </Button>
              <Text size="xs" c="dimmed" ta="center">
                Saving commits the generated Terraform to Git. Nothing is applied to your cloud yet.
              </Text>
            </Stack>
          </Card>
        </Grid.Col>
      </Grid>

      <PreviewDrawer
        opened={previewOpen}
        onClose={() => setPreviewOpen(false)}
        result={preview}
        hasSensitiveVariables={hasSensitive}
        outputPath={path}
        saving={saving}
        onSave={save}
        saveLabel={isEditing ? 'Save changes' : 'Create resource'}
      />
    </>
  );
}

function SectionTitle({ icon, step, title }: { icon: ReactNode; step: number; title: string }) {
  return (
    <Group gap="sm">
      <ThemeIcon variant="light" radius="xl" size={30}>
        {icon}
      </ThemeIcon>
      <Text size="xs" c="dimmed" fw={700} tt="uppercase">
        Step {step}
      </Text>
      <Title order={4}>{title}</Title>
    </Group>
  );
}

function SummaryRow({ label, value }: { label: string; value: ReactNode }) {
  return (
    <Group justify="space-between" wrap="nowrap" gap="sm">
      <Text size="sm" c="dimmed">
        {label}
      </Text>
      {typeof value === 'string' ? (
        <Text size="sm" fw={500} truncate>
          {value}
        </Text>
      ) : (
        value
      )}
    </Group>
  );
}
