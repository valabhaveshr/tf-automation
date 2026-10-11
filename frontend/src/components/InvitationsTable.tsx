import { ActionIcon, Badge, Table, Text, Tooltip, type MantineColor } from '@mantine/core';
import { modals } from '@mantine/modals';
import { notifications } from '@mantine/notifications';
import { IconX } from '@tabler/icons-react';
import * as api from '../api/client';
import type { Invitation, InvitationStatus } from '../api/types';
import { errorMessage, relativeTime } from '../lib/format';

const STATUS_COLOR: Record<InvitationStatus, MantineColor> = {
  pending: 'yellow',
  accepted: 'teal',
  revoked: 'gray',
  expired: 'gray',
};

export default function InvitationsTable({
  invitations,
  showCustomer,
  onChanged,
}: {
  invitations: Invitation[];
  showCustomer?: boolean;
  onChanged: () => void;
}) {
  if (invitations.length === 0) {
    return (
      <Text size="sm" c="dimmed" p="md">
        No invitations yet.
      </Text>
    );
  }

  function revoke(inv: Invitation) {
    modals.openConfirmModal({
      title: 'Revoke invitation?',
      children: <Text size="sm">The link sent to {inv.email} will stop working.</Text>,
      labels: { confirm: 'Revoke', cancel: 'Cancel' },
      confirmProps: { color: 'red' },
      onConfirm: async () => {
        try {
          await api.revokeInvitation(inv.id);
          notifications.show({ color: 'teal', message: 'Invitation revoked.' });
          onChanged();
        } catch (err) {
          notifications.show({ color: 'red', title: 'Revoke failed', message: errorMessage(err, 'Revoke failed.') });
        }
      },
    });
  }

  return (
    <Table.ScrollContainer minWidth={600}>
      <Table verticalSpacing="sm" highlightOnHover>
        <Table.Thead>
          <Table.Tr>
            <Table.Th>Email</Table.Th>
            {showCustomer && <Table.Th>Organization</Table.Th>}
            <Table.Th>Role</Table.Th>
            <Table.Th>Status</Table.Th>
            <Table.Th>Invited</Table.Th>
            <Table.Th w={48} />
          </Table.Tr>
        </Table.Thead>
        <Table.Tbody>
          {invitations.map((inv) => (
            <Table.Tr key={inv.id}>
              <Table.Td>{inv.email}</Table.Td>
              {showCustomer && (
                <Table.Td>
                  {inv.customer_name ?? (
                    <Badge variant="light" color="brand">
                      New customer
                    </Badge>
                  )}
                </Table.Td>
              )}
              <Table.Td>
                <Badge variant="light" color={inv.role === 'customer_admin' ? 'grape' : 'gray'}>
                  {inv.role === 'customer_admin' ? 'Admin' : 'Member'}
                </Badge>
              </Table.Td>
              <Table.Td>
                <Badge variant="dot" color={STATUS_COLOR[inv.status]}>
                  {inv.status}
                </Badge>
              </Table.Td>
              <Table.Td>
                <Text size="sm">{relativeTime(inv.created_at)}</Text>
                <Text size="xs" c="dimmed">
                  by {inv.invited_by}
                </Text>
              </Table.Td>
              <Table.Td>
                {inv.status === 'pending' && (
                  <Tooltip label="Revoke" withArrow>
                    <ActionIcon variant="subtle" color="red" onClick={() => revoke(inv)} aria-label="Revoke invitation">
                      <IconX size={16} />
                    </ActionIcon>
                  </Tooltip>
                )}
              </Table.Td>
            </Table.Tr>
          ))}
        </Table.Tbody>
      </Table>
    </Table.ScrollContainer>
  );
}
