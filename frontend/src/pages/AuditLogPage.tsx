import { useCallback, useEffect, useState } from 'react';
import { Alert, Badge, Button, Card, Code, Group, Select, Table, Text, TextInput, Tooltip } from '@mantine/core';
import { IconHistory, IconSearch } from '@tabler/icons-react';
import * as api from '../api/client';
import type { AuditLogEntry, AuditLogResourceType } from '../api/types';
import { errorMessage, formatDateTime, relativeTime } from '../lib/format';
import { EmptyState, PageHeader } from '../components/common';

const RESOURCE_TYPES: { value: AuditLogResourceType; label: string }[] = [
  { value: 'record', label: 'Resources' },
  { value: 'customer', label: 'Customers' },
  { value: 'invitation', label: 'Invitations' },
  { value: 'catalog', label: 'Catalog' },
  { value: 'naming_template', label: 'Naming templates' },
];
const PAGE_SIZE = 50;

function actionColor(action: string) {
  if (action.endsWith('.delete') || action.endsWith('.revoke')) return 'red';
  if (action.endsWith('.create') || action.endsWith('.onboard')) return 'teal';
  if (action.endsWith('.update')) return 'blue';
  return 'gray';
}

/** Platform admin: who changed what, newest first (GET /admin/audit-log). */
export default function AuditLogPage() {
  const [entries, setEntries] = useState<AuditLogEntry[]>([]);
  const [resourceType, setResourceType] = useState<AuditLogResourceType | null>(null);
  const [actor, setActor] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [hasMore, setHasMore] = useState(true);

  const load = useCallback(
    async (reset: boolean, before?: string) => {
      setLoading(true);
      setError(null);
      try {
        const data = await api.getAuditLog({
          resource_type: resourceType ?? undefined,
          actor: actor.trim() || undefined,
          limit: PAGE_SIZE,
          before: reset ? undefined : before,
        });
        setEntries((prev) => (reset ? data : [...prev, ...data]));
        setHasMore(data.length === PAGE_SIZE);
      } catch (err) {
        setError(errorMessage(err, 'Could not load the audit log.'));
      } finally {
        setLoading(false);
      }
    },
    [resourceType, actor],
  );

  useEffect(() => {
    const t = setTimeout(() => load(true), 250);
    return () => clearTimeout(t);
  }, [load]);

  return (
    <>
      <PageHeader title="Audit log" description="Every state-changing action across all customers, newest first." />
      <Card p={0}>
        <Group p="md" gap="sm">
          <TextInput
            placeholder="Filter by user"
            leftSection={<IconSearch size={16} />}
            value={actor}
            onChange={(e) => setActor(e.currentTarget.value)}
            style={{ flex: 1 }}
          />
          <Select placeholder="All resource types" data={RESOURCE_TYPES} value={resourceType} onChange={(v) => setResourceType(v as AuditLogResourceType | null)} clearable w={220} />
        </Group>
        {error && (
          <Alert color="red" mx="md" mb="md">
            {error}
          </Alert>
        )}
        {entries.length === 0 && !loading ? (
          <EmptyState icon={<IconHistory size={40} stroke={1.3} />} title="No entries" />
        ) : (
          <Table.ScrollContainer minWidth={760}>
            <Table verticalSpacing="sm" highlightOnHover>
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>When</Table.Th>
                  <Table.Th>User</Table.Th>
                  <Table.Th>Action</Table.Th>
                  <Table.Th>Details</Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {entries.map((e) => (
                  <Table.Tr key={e.id}>
                    <Table.Td>
                      <Tooltip label={formatDateTime(e.created_at)} withArrow>
                        <Text size="sm">{relativeTime(e.created_at)}</Text>
                      </Tooltip>
                    </Table.Td>
                    <Table.Td>
                      <Text size="sm" fw={500}>
                        {e.actor_username || '—'}
                      </Text>
                    </Table.Td>
                    <Table.Td>
                      <Badge variant="light" color={actionColor(e.action)} tt="none">
                        {e.action}
                      </Badge>
                    </Table.Td>
                    <Table.Td>
                      {e.detail && Object.keys(e.detail).length > 0 ? (
                        <Group gap={6}>
                          {Object.entries(e.detail).map(([k, v]) => (
                            <Code key={k}>
                              {k}={typeof v === 'string' ? v : JSON.stringify(v)}
                            </Code>
                          ))}
                        </Group>
                      ) : (
                        <Text size="xs" c="dimmed" ff="monospace">
                          {e.resource_id ?? ''}
                        </Text>
                      )}
                    </Table.Td>
                  </Table.Tr>
                ))}
              </Table.Tbody>
            </Table>
          </Table.ScrollContainer>
        )}
        {hasMore && entries.length > 0 && (
          <Group justify="center" p="md">
            <Button variant="default" loading={loading} onClick={() => load(false, entries[entries.length - 1]?.created_at)}>
              Load more
            </Button>
          </Group>
        )}
      </Card>
    </>
  );
}
