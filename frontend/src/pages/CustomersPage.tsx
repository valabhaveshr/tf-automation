import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  Alert,
  Badge,
  Button,
  Card,
  Drawer,
  Group,
  Stack,
  Table,
  Tabs,
  Text,
  TextInput,
} from '@mantine/core';
import { IconBuildingStore, IconMailForward, IconSearch, IconUserPlus } from '@tabler/icons-react';
import * as api from '../api/client';
import type { Customer, Invitation, TeamMember } from '../api/types';
import { errorMessage, formatDateTime, relativeTime, shortCode } from '../lib/format';
import { EmptyState, PageHeader, RoleBadge } from '../components/common';
import InviteModal from '../components/InviteModal';
import InvitationsTable from '../components/InvitationsTable';

/** Platform admin: every customer, invitations, and the "invite a new customer" entry point. */
export default function CustomersPage() {
  const [customers, setCustomers] = useState<Customer[]>([]);
  const [invitations, setInvitations] = useState<Invitation[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [search, setSearch] = useState('');
  const [invite, setInvite] = useState<{ open: boolean; customerId?: string }>({ open: false });
  const [selected, setSelected] = useState<Customer | null>(null);
  const [members, setMembers] = useState<TeamMember[] | null>(null);

  const load = useCallback(async () => {
    try {
      const [c, i] = await Promise.all([api.listCustomers(), api.listInvitations()]);
      setCustomers(c);
      setInvitations(i);
    } catch (err) {
      setError(errorMessage(err, 'Could not load customers.'));
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  useEffect(() => {
    setMembers(null);
    if (selected) api.listCustomerUsers(selected.id).then(setMembers).catch(() => setMembers([]));
  }, [selected]);

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    return customers.filter((c) => !q || [c.name, c.business_short_code, c.country, c.contact_email].some((s) => s.toLowerCase().includes(q)));
  }, [customers, search]);

  const pending = invitations.filter((i) => i.status === 'pending').length;

  return (
    <>
      <PageHeader
        title="Customers"
        description="Organizations using the portal. Invite a new customer's admin; they complete onboarding on first sign-in."
        actions={
          <Button leftSection={<IconUserPlus size={18} />} onClick={() => setInvite({ open: true })}>
            Invite customer
          </Button>
        }
      />
      {error && (
        <Alert color="red" mb="md">
          {error}
        </Alert>
      )}
      <Card p={0}>
        <Tabs defaultValue="customers">
          <Tabs.List px="md" pt="xs">
            <Tabs.Tab value="customers" leftSection={<IconBuildingStore size={16} />}>
              Customers ({customers.length})
            </Tabs.Tab>
            <Tabs.Tab value="invitations" leftSection={<IconMailForward size={16} />}>
              Invitations{pending ? ` (${pending} pending)` : ''}
            </Tabs.Tab>
          </Tabs.List>
          <Tabs.Panel value="customers">
            <Group p="md">
              <TextInput
                placeholder="Search name, short name, country…"
                leftSection={<IconSearch size={16} />}
                value={search}
                onChange={(e) => setSearch(e.currentTarget.value)}
                style={{ flex: 1 }}
              />
            </Group>
            {filtered.length === 0 ? (
              <EmptyState icon={<IconBuildingStore size={40} stroke={1.3} />} title={customers.length ? 'No match' : 'No customers yet'} description={customers.length ? undefined : 'Invite a customer admin to get started.'} />
            ) : (
              <Table.ScrollContainer minWidth={720}>
                <Table verticalSpacing="sm" highlightOnHover>
                  <Table.Thead>
                    <Table.Tr>
                      <Table.Th>Organization</Table.Th>
                      <Table.Th>Short name</Table.Th>
                      <Table.Th>Country</Table.Th>
                      <Table.Th>Primary contact</Table.Th>
                      <Table.Th>Cloud</Table.Th>
                      <Table.Th>Since</Table.Th>
                    </Table.Tr>
                  </Table.Thead>
                  <Table.Tbody>
                    {filtered.map((c) => (
                      <Table.Tr key={c.id} style={{ cursor: 'pointer' }} onClick={() => setSelected(c)}>
                        <Table.Td>
                          <Text fw={600} size="sm">
                            {c.name}
                          </Text>
                          {c.industry && (
                            <Text size="xs" c="dimmed">
                              {c.industry}
                            </Text>
                          )}
                        </Table.Td>
                        <Table.Td>
                          <Badge variant="light" ff="monospace">
                            {shortCode(c.business_short_code)}
                          </Badge>
                        </Table.Td>
                        <Table.Td>{c.country || '—'}</Table.Td>
                        <Table.Td>
                          <Text size="sm">{c.contact_name || '—'}</Text>
                          {c.contact_email && (
                            <Text size="xs" c="dimmed">
                              {c.contact_email}
                            </Text>
                          )}
                        </Table.Td>
                        <Table.Td>
                          <Text size="sm">
                            {(c.default_cloud || 'gcp').toUpperCase()}
                            {c.default_region && (
                              <Text span c="dimmed" size="xs">
                                {' '}
                                · {c.default_region}
                              </Text>
                            )}
                          </Text>
                        </Table.Td>
                        <Table.Td>
                          <Text size="sm">{relativeTime(c.created_at)}</Text>
                        </Table.Td>
                      </Table.Tr>
                    ))}
                  </Table.Tbody>
                </Table>
              </Table.ScrollContainer>
            )}
          </Tabs.Panel>
          <Tabs.Panel value="invitations">
            <InvitationsTable invitations={invitations} showCustomer onChanged={load} />
          </Tabs.Panel>
        </Tabs>
      </Card>

      <Drawer opened={!!selected} onClose={() => setSelected(null)} position="right" size="md" title={<Text fw={600}>{selected?.name}</Text>}>
        {selected && (
          <Stack>
            <Group gap="xs">
              <Badge variant="light" ff="monospace">
                {shortCode(selected.business_short_code)}
              </Badge>
              <Text size="sm" c="dimmed">
                since {formatDateTime(selected.created_at)}
              </Text>
            </Group>
            <Group justify="space-between">
              <Text fw={600}>Users</Text>
              <Button size="xs" variant="light" leftSection={<IconUserPlus size={14} />} onClick={() => setInvite({ open: true, customerId: selected.id })}>
                Invite user
              </Button>
            </Group>
            {members === null ? (
              <Text size="sm" c="dimmed">
                Loading…
              </Text>
            ) : members.length === 0 ? (
              <Text size="sm" c="dimmed">
                No users yet.
              </Text>
            ) : (
              members.map((m) => (
                <Group key={m.id} justify="space-between">
                  <div>
                    <Text size="sm" fw={500}>
                      {m.username}
                    </Text>
                    {m.email && m.email !== m.username && (
                      <Text size="xs" c="dimmed">
                        {m.email}
                      </Text>
                    )}
                  </div>
                  <RoleBadge roles={m.roles} />
                </Group>
              ))
            )}
          </Stack>
        )}
      </Drawer>

      <InviteModal
        opened={invite.open}
        onClose={() => setInvite({ open: false })}
        platformAdmin
        customers={customers}
        presetCustomerId={invite.customerId}
        onCreated={() => load()}
      />
    </>
  );
}
