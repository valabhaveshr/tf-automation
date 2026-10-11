import { useEffect, useState, type FormEvent } from 'react';
import {
  Alert,
  Button,
  Code,
  CopyButton,
  Group,
  Modal,
  SegmentedControl,
  Select,
  Stack,
  Text,
  TextInput,
} from '@mantine/core';
import { IconCheck, IconCopy, IconMailForward } from '@tabler/icons-react';
import * as api from '../api/client';
import type { Customer, Invitation, InvitationRole } from '../api/types';
import { errorMessage, formatDateTime, shortCode } from '../lib/format';

const NEW_CUSTOMER = '__new__';

/**
 * Creates an invitation and shows the one-time link. There's no email
 * delivery yet, so the inviter copies the link and sends it themselves.
 *
 * - Platform admin: defaults to inviting a new customer's first admin (they
 *   complete onboarding on first login), or can add a user to an existing
 *   customer.
 * - Customer admin: invites a member or another admin into their own
 *   organization.
 */
export default function InviteModal({
  opened,
  onClose,
  platformAdmin,
  customers = [],
  presetCustomerId,
  onCreated,
}: {
  opened: boolean;
  onClose: () => void;
  platformAdmin: boolean;
  customers?: Customer[];
  presetCustomerId?: string;
  onCreated?: (inv: Invitation) => void;
}) {
  const [email, setEmail] = useState('');
  const [target, setTarget] = useState<string>(NEW_CUSTOMER);
  const [role, setRole] = useState<InvitationRole>('requester');
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [created, setCreated] = useState<Invitation | null>(null);

  useEffect(() => {
    if (!opened) return;
    setEmail('');
    setError(null);
    setCreated(null);
    setTarget(presetCustomerId ?? (platformAdmin ? NEW_CUSTOMER : ''));
    setRole(platformAdmin && !presetCustomerId ? 'customer_admin' : 'requester');
  }, [opened, platformAdmin, presetCustomerId]);

  const forNewCustomer = platformAdmin && target === NEW_CUSTOMER;

  async function submit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    setSubmitting(true);
    try {
      const inv = await api.createInvitation({
        email: email.trim(),
        role: forNewCustomer ? 'customer_admin' : role,
        customer_id: platformAdmin && !forNewCustomer ? target : undefined,
      });
      setCreated(inv);
      onCreated?.(inv);
    } catch (err) {
      setError(errorMessage(err, 'Could not create the invitation.'));
    } finally {
      setSubmitting(false);
    }
  }

  const link = created?.invite_path ? `${window.location.origin}${created.invite_path}` : '';

  return (
    <Modal opened={opened} onClose={onClose} title={<Text fw={600}>Invite someone</Text>} size="lg" centered>
      {created ? (
        <Stack>
          <Alert color="teal" icon={<IconCheck size={18} />} title="Invitation created">
            Send this link to <b>{created.email}</b>. It can be used once and expires {formatDateTime(created.expires_at)}.
            {forNewCustomer && ' They will set up their organization on first sign-in.'}
          </Alert>
          <Code block style={{ wordBreak: 'break-all', whiteSpace: 'pre-wrap' }}>
            {link}
          </Code>
          <Text size="xs" c="dimmed">
            The link is shown only now. If it's lost, revoke this invitation and create a new one.
          </Text>
          <Group justify="flex-end">
            <CopyButton value={link}>
              {({ copied, copy }) => (
                <Button leftSection={copied ? <IconCheck size={16} /> : <IconCopy size={16} />} color={copied ? 'teal' : undefined} onClick={copy}>
                  {copied ? 'Copied' : 'Copy link'}
                </Button>
              )}
            </CopyButton>
            <Button variant="default" onClick={onClose}>
              Done
            </Button>
          </Group>
        </Stack>
      ) : (
        <form onSubmit={submit}>
          <Stack>
            {platformAdmin && (
              <Select
                label="Organization"
                data={[
                  { value: NEW_CUSTOMER, label: 'New customer (they complete onboarding)' },
                  ...customers.map((c) => ({ value: c.id, label: `${c.name} (${shortCode(c.business_short_code)})` })),
                ]}
                value={target}
                onChange={(v) => setTarget(v ?? NEW_CUSTOMER)}
                allowDeselect={false}
                searchable
              />
            )}
            <TextInput
              label="Email address"
              placeholder="name@company.com"
              type="email"
              required
              value={email}
              onChange={(e) => setEmail(e.currentTarget.value)}
              data-autofocus
            />
            {!forNewCustomer && (
              <div>
                <Text size="sm" fw={500} mb={4}>
                  Role
                </Text>
                <SegmentedControl
                  value={role}
                  onChange={(v) => setRole(v as InvitationRole)}
                  data={[
                    { value: 'requester', label: 'Member: requests resources' },
                    { value: 'customer_admin', label: 'Admin: also manages team & profile' },
                  ]}
                  fullWidth
                />
              </div>
            )}
            {forNewCustomer && (
              <Text size="sm" c="dimmed">
                They'll be the new customer's admin, and can invite their own team afterwards.
              </Text>
            )}
            {error && <Alert color="red">{error}</Alert>}
            <Group justify="flex-end">
              <Button variant="default" onClick={onClose}>
                Cancel
              </Button>
              <Button type="submit" loading={submitting} leftSection={<IconMailForward size={16} />}>
                Create invitation
              </Button>
            </Group>
          </Stack>
        </form>
      )}
    </Modal>
  );
}
