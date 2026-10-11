import { useEffect, useState, type FormEvent } from 'react';
import { Alert, Badge, Button, Group, Loader, PasswordInput, Stack, Text, TextInput } from '@mantine/core';
import { IconAlertCircle } from '@tabler/icons-react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import * as api from '../api/client';
import type { InvitationPreview } from '../api/types';
import { useAuth } from '../state/auth';
import { errorMessage, formatDateTime } from '../lib/format';
import { AuthScreen } from './LoginPage';

export default function AcceptInvitePage() {
  const { token = '' } = useParams<{ token: string }>();
  const { refresh, me, logout } = useAuth();
  const navigate = useNavigate();
  const [invite, setInvite] = useState<InvitationPreview | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    api
      .previewInvitation(token)
      .then((inv) => {
        setInvite(inv);
        setUsername(inv.email);
      })
      .catch((err) => setLoadError(errorMessage(err, 'This invitation could not be loaded.')));
  }, [token]);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    if (password.length < 8) return setError('Use at least 8 characters for your password.');
    if (password !== confirm) return setError('The passwords do not match.');
    setSubmitting(true);
    try {
      await api.acceptInvitation(token, { username: username.trim(), password });
      const session = await refresh();
      navigate(session?.needs_onboarding ? '/onboarding' : '/', { replace: true });
    } catch (err) {
      setError(errorMessage(err, 'Could not accept the invitation.'));
    } finally {
      setSubmitting(false);
    }
  }

  if (loadError) {
    return (
      <AuthScreen title="Invitation unavailable">
        <Stack>
          <Alert color="red" icon={<IconAlertCircle size={18} />}>
            {loadError}
          </Alert>
          <Text size="sm" c="dimmed">
            Ask whoever invited you for a new link.
          </Text>
          <Button component={Link} to="/login" variant="default">
            Go to sign in
          </Button>
        </Stack>
      </AuthScreen>
    );
  }

  if (!invite) {
    return (
      <AuthScreen title="Checking your invitation">
        <Group justify="center">
          <Loader />
        </Group>
      </AuthScreen>
    );
  }

  const newCustomer = !invite.customer_name;

  return (
    <AuthScreen
      title="Create your account"
      subtitle={newCustomer ? "You've been invited to set up your organization on the Infra Portal." : `You've been invited to join ${invite.customer_name}.`}
      width={460}
    >
      {me && (
        <Alert color="yellow" mb="md">
          You're signed in as <b>{me.user.username}</b>.{' '}
          <Button variant="subtle" size="compact-xs" onClick={() => logout()}>
            Sign out first
          </Button>
        </Alert>
      )}
      <form onSubmit={submit}>
        <Stack>
          <Group gap="xs">
            <Text size="sm" c="dimmed">
              Invited as
            </Text>
            <Badge variant="light" color={invite.role === 'customer_admin' ? 'grape' : 'gray'}>
              {invite.role === 'customer_admin' ? 'Customer admin' : 'Member'}
            </Badge>
          </Group>
          <TextInput
            label="Username"
            description="Letters, digits, '.', '_', '@' or '-'. Your email works fine."
            value={username}
            onChange={(e) => setUsername(e.currentTarget.value)}
            required
            autoComplete="username"
          />
          <PasswordInput label="Password" description="At least 8 characters." value={password} onChange={(e) => setPassword(e.currentTarget.value)} required autoComplete="new-password" />
          <PasswordInput label="Confirm password" value={confirm} onChange={(e) => setConfirm(e.currentTarget.value)} required autoComplete="new-password" />
          {error && (
            <Alert color="red" icon={<IconAlertCircle size={18} />}>
              {error}
            </Alert>
          )}
          <Button type="submit" loading={submitting} size="md" fullWidth disabled={!!me}>
            {newCustomer ? 'Create account and continue' : 'Create account'}
          </Button>
          <Text size="xs" c="dimmed" ta="center">
            This invitation expires {formatDateTime(invite.expires_at)}.
          </Text>
        </Stack>
      </form>
    </AuthScreen>
  );
}
