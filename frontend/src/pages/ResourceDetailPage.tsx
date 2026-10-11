import { useCallback, useEffect, useState, type ReactNode } from 'react';
import {
  Alert,
  Anchor,
  Badge,
  Breadcrumbs,
  Button,
  Card,
  Code,
  Group,
  Loader,
  SimpleGrid,
  Stack,
  Table,
  Text,
  Title,
} from '@mantine/core';
import { modals } from '@mantine/modals';
import { notifications } from '@mantine/notifications';
import { IconCode, IconEdit, IconRefresh, IconTrash } from '@tabler/icons-react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import * as api from '../api/client';
import type { ModuleSchema, PreviewResult, TfRecord } from '../api/types';
import { displayValue, envColor, errorMessage, formatDateTime, relativeTime } from '../lib/format';
import { humanize, instanceLabel, versionFromTag } from '../lib/schemaForm';
import { MODULE_ACCENTS } from '../theme';
import { CloudBadge, ModuleIcon, StatusBadge } from '../components/common';
import PreviewDrawer from '../components/PreviewDrawer';

export default function ResourceDetailPage() {
  const { id = '' } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [record, setRecord] = useState<TfRecord | null>(null);
  const [schemas, setSchemas] = useState<Record<string, ModuleSchema>>({});
  const [error, setError] = useState<string | null>(null);
  const [preview, setPreview] = useState<PreviewResult | null>(null);
  const [previewOpen, setPreviewOpen] = useState(false);
  const [loadingPreview, setLoadingPreview] = useState(false);
  const [retrying, setRetrying] = useState(false);

  const load = useCallback(async () => {
    try {
      const rec = await api.getRecord(id);
      setRecord(rec);
      const entries = await Promise.all(
        rec.module_selections.map(async (sel) => {
          try {
            return [sel.module_name, await api.getModuleSchema(sel.module_name, sel.pinned_version ?? undefined)] as const;
          } catch {
            return null;
          }
        }),
      );
      setSchemas(Object.fromEntries(entries.filter((e): e is NonNullable<typeof e> => !!e)));
    } catch (err) {
      setError(errorMessage(err, 'Could not load this resource.'));
    }
  }, [id]);

  useEffect(() => {
    load();
  }, [load]);

  async function showCode() {
    if (!record) return;
    setLoadingPreview(true);
    try {
      const result = await api.previewRecordEdit(record.id, {
        customer_id: record.customer_id,
        customer_name: record.customer_name,
        project_name: record.project_name,
        env_name: record.env_name,
        module_selections: record.module_selections,
      });
      setPreview(result);
      setPreviewOpen(true);
    } catch (err) {
      notifications.show({ color: 'red', title: 'Could not render the code', message: errorMessage(err, 'Preview failed.') });
    } finally {
      setLoadingPreview(false);
    }
  }

  async function retry() {
    setRetrying(true);
    try {
      await api.retrySync(id);
      notifications.show({ color: 'teal', message: 'Sync retried.' });
      await load();
    } catch (err) {
      notifications.show({ color: 'red', title: 'Retry failed', message: errorMessage(err, 'Retry failed.') });
    } finally {
      setRetrying(false);
    }
  }

  function confirmDelete() {
    modals.openConfirmModal({
      title: 'Delete resource request?',
      children: (
        <Text size="sm">
          This removes it from the portal. Files already committed to the output repository are <b>not</b> deleted.
        </Text>
      ),
      labels: { confirm: 'Delete', cancel: 'Cancel' },
      confirmProps: { color: 'red' },
      onConfirm: async () => {
        try {
          await api.deleteRecord(id);
          notifications.show({ color: 'teal', message: 'Resource request deleted.' });
          navigate('/');
        } catch (err) {
          notifications.show({ color: 'red', title: 'Delete failed', message: errorMessage(err, 'Delete failed.') });
        }
      },
    });
  }

  if (error) return <Alert color="red">{error}</Alert>;
  if (!record) {
    return (
      <Group justify="center" py="xl">
        <Loader />
      </Group>
    );
  }

  const totalInstances = record.module_selections.reduce((n, m) => n + Object.keys(m.instances ?? {}).length, 0);

  return (
    <>
      <Breadcrumbs mb="sm">
        <Anchor component={Link} to="/" size="sm">
          Resources
        </Anchor>
        <Text size="sm">
          {record.project_name} / {record.env_name}
        </Text>
      </Breadcrumbs>

      <Group justify="space-between" align="flex-start" mb="lg" wrap="wrap">
        <Stack gap={6}>
          <Group gap="sm">
            <Title order={2}>{record.project_name}</Title>
            <Badge variant="light" color={envColor(record.env_name)} size="lg">
              {record.env_name}
            </Badge>
            <StatusBadge status={record.sync_status} />
          </Group>
          <Text c="dimmed" size="sm">
            {record.customer_name} · {record.module_selections.length} module{record.module_selections.length === 1 ? '' : 's'} · {totalInstances}{' '}
            instance{totalInstances === 1 ? '' : 's'} · updated {relativeTime(record.updated_at)} by {record.updated_by}
          </Text>
        </Stack>
        <Group gap="sm">
          <Button variant="default" leftSection={<IconCode size={18} />} onClick={showCode} loading={loadingPreview}>
            View generated code
          </Button>
          {record.sync_status === 'failed' && (
            <Button variant="light" color="orange" leftSection={<IconRefresh size={18} />} onClick={retry} loading={retrying}>
              Retry sync
            </Button>
          )}
          <Button leftSection={<IconEdit size={18} />} component={Link} to={`/resources/${record.id}/edit`}>
            Edit
          </Button>
          <Button variant="subtle" color="red" leftSection={<IconTrash size={18} />} onClick={confirmDelete}>
            Delete
          </Button>
        </Group>
      </Group>

      {record.sync_status === 'failed' && (
        <Alert color="orange" mb="lg" title="Not committed to Git">
          The request is saved, but committing the generated files to the output repository failed. Use “Retry sync” once the issue is resolved.
        </Alert>
      )}

      <SimpleGrid cols={{ base: 1, sm: 2, lg: 4 }} mb="lg">
        <Info label="Output path" value={<Code>{record.output_path}/</Code>} />
        <Info label="Last commit" value={record.last_commit_sha ? <Code>{record.last_commit_sha.slice(0, 10)}</Code> : '—'} />
        <Info label="Created" value={`${formatDateTime(record.created_at)} by ${record.created_by}`} />
        <Info label="Last updated" value={`${formatDateTime(record.updated_at)} by ${record.updated_by}`} />
      </SimpleGrid>

      <Stack gap="lg">
        {record.module_selections.map((sel, i) => {
          const schema = schemas[sel.module_name];
          const meta = schema?.metadata;
          const accent = MODULE_ACCENTS[i % MODULE_ACCENTS.length];
          const instances = Object.entries(sel.instances ?? {});
          return (
            <Card key={sel.module_name} p="lg" style={{ borderLeft: `5px solid var(--mantine-color-${accent}-6)` }}>
              <Group justify="space-between" mb="md" wrap="wrap">
                <Group gap="sm">
                  <ModuleIcon icon={meta?.icon} category={meta?.category} color={accent} />
                  <div>
                    <Group gap={8}>
                      <Title order={4}>{meta?.display_name ?? sel.module_name}</Title>
                      {meta?.cloud && <CloudBadge cloud={meta.cloud} short />}
                      <Badge variant="outline" color="gray">
                        {versionFromTag(sel.pinned_version)}
                      </Badge>
                    </Group>
                    <Text size="xs" c="dimmed" ff="monospace">
                      {sel.module_name}
                    </Text>
                  </div>
                </Group>
                <Badge variant="light" color={accent} size="lg">
                  {instances.length} instance{instances.length === 1 ? '' : 's'}
                </Badge>
              </Group>
              <Table.ScrollContainer minWidth={560}>
                <Table verticalSpacing="xs" withTableBorder withColumnBorders>
                  <Table.Thead>
                    <Table.Tr>
                      <Table.Th w={180}>Instance</Table.Th>
                      <Table.Th>Settings (values that differ from module defaults)</Table.Th>
                    </Table.Tr>
                  </Table.Thead>
                  <Table.Tbody>
                    {instances.map(([name, values]) => {
                      const label = schema ? instanceLabel(schema, values) : null;
                      const sensitive = new Set(schema?.variables.filter((v) => v.sensitive).map((v) => v.name));
                      return (
                        <Table.Tr key={name}>
                          <Table.Td valign="top">
                            <Text ff="monospace" fw={600} size="sm">
                              {name}
                            </Text>
                            {label && label !== name && (
                              <Text size="xs" c="dimmed">
                                {label}
                              </Text>
                            )}
                          </Table.Td>
                          <Table.Td>
                            <Group gap={6}>
                              {Object.entries(values).length === 0 && (
                                <Text size="sm" c="dimmed">
                                  All defaults
                                </Text>
                              )}
                              {Object.entries(values).map(([k, v]) => (
                                <Badge key={k} variant="default" radius="sm" tt="none" fw={400} size="lg" styles={{ label: { overflow: 'visible' } }}>
                                  <Text span size="xs" c="dimmed">
                                    {humanize(k)}:
                                  </Text>{' '}
                                  <Text span size="xs" ff="monospace">
                                    {sensitive.has(k) ? '••••••' : truncate(displayValue(v), 60)}
                                  </Text>
                                </Badge>
                              ))}
                            </Group>
                          </Table.Td>
                        </Table.Tr>
                      );
                    })}
                  </Table.Tbody>
                </Table>
              </Table.ScrollContainer>
            </Card>
          );
        })}
      </Stack>

      <PreviewDrawer
        opened={previewOpen}
        onClose={() => setPreviewOpen(false)}
        result={preview ? { ...preview, diff: null } : null}
        hasSensitiveVariables={Object.values(schemas).some((s) => s.variables.some((v) => v.sensitive))}
        outputPath={record.output_path}
      />
    </>
  );
}

function truncate(s: string, n: number) {
  return s.length > n ? s.slice(0, n - 1) + '…' : s;
}

function Info({ label, value }: { label: string; value: ReactNode }) {
  return (
    <Card p="md">
      <Text size="xs" c="dimmed" tt="uppercase" fw={700} mb={4}>
        {label}
      </Text>
      {typeof value === 'string' ? <Text size="sm">{value}</Text> : value}
    </Card>
  );
}
