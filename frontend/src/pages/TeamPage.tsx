import { useCallback, useEffect, useState } from 'react';
import { Alert, Avatar, Button, Card, Group, Table, Tabs, Text } from '@mantine/core';
import { IconMailForward, IconUserPlus, IconUsers } from '@tabler/icons-react';
import * as api from '../api/client';
import type { Invitation, TeamMember } from '../api/types';
import { useAuth } from '../state/auth';
import { errorMessage, formatDateTime } from '../lib/format';
import { PageHeader, RoleBadge } from '../components/common';
import InviteModal from '../components/InviteModal';
import InvitationsTable from '../components/InvitationsTable';

/** The customer's members; customer admins can also invite people and manage invitations. */
export default function TeamPage() {
  const { me } = useAuth();
  const isAdmin = !!me?.is_customer_admin;
  const [members, setMembers] = useState<TeamMember[]>([]);
  const [invitations, setInvitations] = useState<Invitation[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [inviteOpen, setInviteOpen] = useState(false);

  const load = useCallback(async () => {
    try {
      setMembers(await api.listTeam());
      if (isAdmin) setInvitations(await api.listInvitations());
    } catch (err) {
      setError(errorMessage(err, 'Could not load the team.'));
    }
  }, [isAdmin]);

  useEffect(() => {
    load();
  }, [load]);

  const pending = invitations.filter((i) => i.status === 'pending').length;

  return (
    <>
      <PageHeader
        title="Team"
        description={`People who can request resources for ${me?.customer?.name ?? 'your organization'}.`}
        actions={
          isAdmin && (
            <Button leftSection={<IconUserPlus size={18} />} onClick={() => setInviteOpen(true)}>
              Invite people
            </Button>
          )
        }
      />
      {error && (
        <Alert color="red" mb="md">
          {error}
        </Alert>
      )}
      <Card p={0}>
        <Tabs defaultValue="members">
          <Tabs.List px="md" pt="xs">
            <Tabs.Tab value="members" leftSection={<IconUsers size={16} />}>
              Members ({members.length})
            </Tabs.Tab>
            {isAdmin && (
              <Tabs.Tab value="invitations" leftSection={<IconMailForward size={16} />}>
                Invitations{pending ? ` (${pending} pending)` : ''}
              </Tabs.Tab>
            )}
          </Tabs.List>
          <Tabs.Panel value="members">
            <Table.ScrollContainer minWidth={520}>
              <Table verticalSpacing="sm" highlightOnHover>
                <Table.Thead>
                  <Table.Tr>
                    <Table.Th>User</Table.Th>
                    <Table.Th>Role</Table.Th>
                    <Table.Th>Joined</Table.Th>
                  </Table.Tr>
                </Table.Thead>
                <Table.Tbody>
                  {members.map((m) => (
                    <Table.Tr key={m.id}>
                      <Table.Td>
                        <Group gap="sm" wrap="nowrap">
                          <Avatar radius="xl" size={32} color="brand">
                            {m.username.slice(0, 2).toUpperCase()}
                          </Avatar>
                          <div>
                            <Text size="sm" fw={500}>
                              {m.username}
                              {m.id === me?.user.id && (
                                <Text span c="dimmed" size="xs">
                                  {' '}
                                  (you)
                                </Text>
                              )}
                            </Text>
                            {m.email && m.email !== m.username && (
                              <Text size="xs" c="dimmed">
                                {m.email}
                              </Text>
                            )}
                          </div>
                        </Group>
                      </Table.Td>
                      <Table.Td>
                        <RoleBadge roles={m.roles} />
                      </Table.Td>
                      <Table.Td>
                        <Text size="sm">{formatDateTime(m.created_at)}</Text>
                      </Table.Td>
                    </Table.Tr>
                  ))}
                </Table.Tbody>
              </Table>
            </Table.ScrollContainer>
          </Tabs.Panel>
          {isAdmin && (
            <Tabs.Panel value="invitations">
              <InvitationsTable invitations={invitations} onChanged={load} />
            </Tabs.Panel>
          )}
        </Tabs>
      </Card>
      <InviteModal opened={inviteOpen} onClose={() => setInviteOpen(false)} platformAdmin={false} onCreated={() => load()} />
    </>
  );
}
