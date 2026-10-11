import { Button, Stack, Text } from '@mantine/core';
import { useNavigate } from 'react-router-dom';
import { useAuth } from '../state/auth';
import { AuthScreen } from './LoginPage';

/** Signed in, but not linked to any customer (e.g. an SSO user nobody has invited yet). */
export default function NoOrganizationPage() {
  const { me, logout } = useAuth();
  const navigate = useNavigate();
  return (
    <AuthScreen title="No organization yet">
      <Stack>
        <Text size="sm">
          You're signed in as <b>{me?.user.username}</b>, but your account isn't part of an organization yet.
        </Text>
        <Text size="sm" c="dimmed">
          Ask your organization's admin (or the platform team) for an invitation link, then open it to join.
        </Text>
        <Button
          variant="default"
          onClick={async () => {
            await logout();
            navigate('/login');
          }}
        >
          Sign out
        </Button>
      </Stack>
    </AuthScreen>
  );
}
