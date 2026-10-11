import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react';
import {
  ActionIcon,
  Alert,
  Badge,
  Button,
  Card,
  Group,
  Menu,
  SegmentedControl,
  Select,
  SimpleGrid,
  Skeleton,
  Stack,
  Table,
  Text,
  TextInput,
  ThemeIcon,
  Tooltip,
} from '@mantine/core';
import { modals } from '@mantine/modals';
import { notifications } from '@mantine/notifications';
import {
  IconAlertTriangle,
  IconCircleCheck,
  IconDots,
  IconEdit,
  IconEye,
  IconFolders,
  IconPlus,
  IconRefresh,
  IconSearch,
  IconServer2,
  IconStack2,
  IconTrash,
} from '@tabler/icons-react';
import { Link, useNavigate } from 'react-router-dom';
import * as api from '../api/client';
import type { Customer, RecordSummary } from '../api/types';
import { useAuth } from '../state/auth';
import { envColor, errorMessage, relativeTime, shortCode } from '../lib/format';
import { EmptyState, PageHeader, StatusBadge } from '../components/common';

type StatusFilter = 'all' | 'synced' | 'pending' | 'failed';

/** Home: every resource request for the signed-in customer (platform admins: all customers, filterable). */
export default function ResourcesPage() {
  const { me } = useAuth();
  const navigate = useNavigate();
  const platform = !!me?.is_platform_admin;
  const [records, setRecords] = useState<RecordSummary[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [customers, setCustomers] = useState<Customer[]>([]);
  const [customerId, setCustomerId] = useState<string | null>(null);
  const [search, setSearch] = useState('');
  const [env, setEnv] = useState<string | null>(null);
  const [status, setStatus] = useState<StatusFilter>('all');

  const load = useCallback(async () => {
    setError(null);
    try {
      setRecords(await api.listRecords({ customer_id: customerId ?? undefined }));
    } catch (err) {
      setError(errorMessage(err, 'Could not load resources.'));
      setRecords([]);
    }
  }, [customerId]);

  useEffect(() => {
    load();
  }, [load]);

  useEffect(() => {
    if (platform) api.listCustomers().then(setCustomers).catch(() => setCustomers([]));
  }, [platform]);

  const envs = useMemo(() => [...new Set((records ?? []).map((r) => r.env_name))].sort(), [records]);
  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    return (records ?? []).filter(
      (r) =>
        (!q || [r.project_name, r.env_name, r.customer_name, r.output_path].some((s) => s.toLowerCase().includes(q))) &&
        (!env || r.env_name === env) &&
        (status === 'all' || r.sync_status === status),
    );
  }, [records, search, env, status]);

  const stats = useMemo(() => {
    const list = records ?? [];
    return {
      total: list.length,
      projects: new Set(list.map((r) => `${r.customer_id}/${r.project_name}`)).size,
      synced: list.filter((r) => r.sync_status === 'synced').length,
      failed: list.filter((r) => r.sync_status === 'failed').length,
    };
  }, [records]);

  function confirmDelete(r: RecordSummary) {
    modals.openConfirmModal({
      title: 'Delete resource request?',
      children: (
        <Stack gap="xs">
          <Text size="sm">
            <b>
              {r.project_name} / {r.env_name}
            </b>{' '}
            will be removed from the portal.
          </Text>
          <Text size="sm" c="dimmed">
            Files already committed to the output repository are not deleted.
          </Text>
        </Stack>
      ),
      labels: { confirm: 'Delete', cancel: 'Cancel' },
      confirmProps: { color: 'red' },
      onConfirm: async () => {
        try {
          await api.deleteRecord(r.id);
          notifications.show({ color: 'teal', message: 'Resource request deleted.' });
          load();
        } catch (err) {
          notifications.show({ color: 'red', title: 'Delete failed', message: errorMessage(err, 'Delete failed.') });
        }
      },
    });
  }

  async function retry(r: RecordSummary) {
    try {
      await api.retrySync(r.id);
      notifications.show({ color: 'teal', message: 'Sync retried.' });
      load();
    } catch (err) {
      notifications.show({ color: 'red', title: 'Retry failed', message: errorMessage(err, 'Retry failed.') });
    }
  }

  const heading = me?.customer ? `${me.customer.name} resources` : 'All resources';

  return (
    <>
      <PageHeader
        title={heading}
        description="Each resource request is one project and environment, and becomes one Terraform root module in Git."
        actions={
          <>
            <Tooltip label="Refresh" withArrow>
              <ActionIcon variant="default" size="lg" onClick={load} aria-label="Refresh">
                <IconRefresh size={18} />
              </ActionIcon>
            </Tooltip>
            <Button leftSection={<IconPlus size={18} />} component={Link} to="/resources/new">
              New resource
            </Button>
          </>
        }
      />

      <SimpleGrid cols={{ base: 2, md: 4 }} mb="lg">
        <StatCard icon={<IconStack2 size={20} />} color="brand" label="Resource requests" value={stats.total} loading={!records} />
        <StatCard icon={<IconFolders size={20} />} color="grape" label="Projects" value={stats.projects} loading={!records} />
        <StatCard icon={<IconCircleCheck size={20} />} color="teal" label="Synced to Git" value={stats.synced} loading={!records} />
        <StatCard icon={<IconAlertTriangle size={20} />} color="red" label="Need attention" value={stats.failed} loading={!records} />
      </SimpleGrid>

      <Card p={0}>
        <Group p="md" gap="sm" wrap="wrap">
          <TextInput
            placeholder="Search project, environment, path…"
            leftSection={<IconSearch size={16} />}
            value={search}
            onChange={(e) => setSearch(e.currentTarget.value)}
            style={{ flex: 1, minWidth: 220 }}
          />
          {platform && (
            <Select
              placeholder="All customers"
              data={customers.map((c) => ({ value: c.id, label: `${c.name} (${shortCode(c.business_short_code)})` }))}
              value={customerId}
              onChange={setCustomerId}
              clearable
              searchable
              w={260}
            />
          )}
          <Select placeholder="All environments" data={envs} value={env} onChange={setEnv} clearable w={180} />
          <SegmentedControl
            value={status}
            onChange={(v) => setStatus(v as StatusFilter)}
            data={[
              { value: 'all', label: 'All' },
              { value: 'synced', label: 'Synced' },
              { value: 'pending', label: 'Pending' },
              { value: 'failed', label: 'Failed' },
            ]}
          />
        </Group>

        {error && (
          <Alert color="red" mx="md" mb="md">
            {error}
          </Alert>
        )}

        {!records ? (
          <Stack p="md" gap="xs">
            {[0, 1, 2].map((i) => (
              <Skeleton key={i} h={44} />
            ))}
          </Stack>
        ) : filtered.length === 0 ? (
          records.length === 0 ? (
            <EmptyState
              icon={<IconServer2 size={44} stroke={1.3} />}
              title="No resources yet"
              description="Request your first infrastructure: pick modules, fill in their settings, preview the generated Terraform and save."
              action={
                <Button leftSection={<IconPlus size={16} />} component={Link} to="/resources/new">
                  New resource
                </Button>
              }
            />
          ) : (
            <EmptyState icon={<IconSearch size={40} stroke={1.3} />} title="Nothing matches these filters" />
          )
        ) : (
          <Table.ScrollContainer minWidth={720}>
            <Table highlightOnHover verticalSpacing="sm" horizontalSpacing="md">
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>Project</Table.Th>
                  <Table.Th>Environment</Table.Th>
                  {platform && <Table.Th>Customer</Table.Th>}
                  <Table.Th>Output path</Table.Th>
                  <Table.Th>Status</Table.Th>
                  <Table.Th>Updated</Table.Th>
                  <Table.Th w={48} />
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {filtered.map((r) => (
                  <Table.Tr key={r.id} style={{ cursor: 'pointer' }} onClick={() => navigate(`/resources/${r.id}`)}>
                    <Table.Td>
                      <Text fw={600}>{r.project_name}</Text>
                    </Table.Td>
                    <Table.Td>
                      <Badge variant="light" color={envColor(r.env_name)}>
                        {r.env_name}
                      </Badge>
                    </Table.Td>
                    {platform && <Table.Td>{r.customer_name}</Table.Td>}
                    <Table.Td>
                      <Text size="sm" ff="monospace" c="dimmed">
                        {r.output_path}
                      </Text>
                    </Table.Td>
                    <Table.Td>
                      <StatusBadge status={r.sync_status} />
                    </Table.Td>
                    <Table.Td>
                      <Tooltip label={new Date(r.updated_at).toLocaleString()} withArrow>
                        <Text size="sm">{relativeTime(r.updated_at)}</Text>
                      </Tooltip>
                    </Table.Td>
                    <Table.Td onClick={(e) => e.stopPropagation()}>
                      <Menu position="bottom-end" withArrow shadow="md">
                        <Menu.Target>
                          <ActionIcon variant="subtle" color="gray" aria-label="Actions">
                            <IconDots size={18} />
                          </ActionIcon>
                        </Menu.Target>
                        <Menu.Dropdown>
                          <Menu.Item leftSection={<IconEye size={16} />} component={Link} to={`/resources/${r.id}`}>
                            View
                          </Menu.Item>
                          <Menu.Item leftSection={<IconEdit size={16} />} component={Link} to={`/resources/${r.id}/edit`}>
                            Edit
                          </Menu.Item>
                          {r.sync_status === 'failed' && (
                            <Menu.Item leftSection={<IconRefresh size={16} />} onClick={() => retry(r)}>
                              Retry sync
                            </Menu.Item>
                          )}
                          <Menu.Divider />
                          <Menu.Item color="red" leftSection={<IconTrash size={16} />} onClick={() => confirmDelete(r)}>
                            Delete
                          </Menu.Item>
                        </Menu.Dropdown>
                      </Menu>
                    </Table.Td>
                  </Table.Tr>
                ))}
              </Table.Tbody>
            </Table>
          </Table.ScrollContainer>
        )}
      </Card>
    </>
  );
}

function StatCard({
  icon,
  color,
  label,
  value,
  loading,
}: {
  icon: ReactNode;
  color: string;
  label: string;
  value: number;
  loading: boolean;
}) {
  return (
    <Card p="md">
      <Group justify="space-between" wrap="nowrap">
        <div>
          <Text size="xs" c="dimmed" tt="uppercase" fw={700}>
            {label}
          </Text>
          {loading ? <Skeleton h={28} w={40} mt={4} /> : <Text fz={28} fw={700} lh={1.2}>{value}</Text>}
        </div>
        <ThemeIcon variant="light" color={color} size={42} radius="md">
          {icon}
        </ThemeIcon>
      </Group>
    </Card>
  );
}
