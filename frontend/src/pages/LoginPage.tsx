import { useState, type FormEvent, type ReactNode } from 'react';
import { Alert, Button, Center, Divider, Paper, PasswordInput, Stack, Text, TextInput, ThemeIcon, Title } from '@mantine/core';
import { IconAlertCircle, IconStack2 } from '@tabler/icons-react';
import { Navigate, useLocation, useNavigate } from 'react-router-dom';
import { ApiError, ssoLoginUrl } from '../api/client';
import { useAuth } from '../state/auth';
import { errorMessage } from '../lib/format';
import classes from './AuthScreen.module.css';

export default function LoginPage() {
  const { me, login } = useAuth();
  const navigate = useNavigate();
  const location = useLocation();
  const from = (location.state as { from?: string } | null)?.from ?? '/';
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  if (me) return <Navigate to={from} replace />;

  async function submit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    setLoading(true);
    try {
      const session = await login(username, password);
      if (!session) {
        // Signed in, but the browser didn't keep the session cookie.
        setError(
          'Signed in, but your browser did not keep the session cookie. Open the portal via https or http://localhost, or ask your administrator to set COOKIE_SECURE=false for this test deployment.',
        );
        return;
      }
      navigate(from, { replace: true });
    } catch (err) {
      setError(err instanceof ApiError && err.status === 401 ? 'Incorrect username or password.' : errorMessage(err, 'Sign-in failed.'));
    } finally {
      setLoading(false);
    }
  }

  return (
    <AuthScreen title="Sign in" subtitle="Self-service infrastructure for your organization">
      <form onSubmit={submit}>
        <Stack>
          <TextInput label="Username" value={username} onChange={(e) => setUsername(e.currentTarget.value)} required autoComplete="username" autoFocus />
          <PasswordInput label="Password" value={password} onChange={(e) => setPassword(e.currentTarget.value)} required autoComplete="current-password" />
          {error && (
            <Alert color="red" icon={<IconAlertCircle size={18} />}>
              {error}
            </Alert>
          )}
          <Button type="submit" loading={loading} fullWidth size="md">
            Sign in
          </Button>
        </Stack>
      </form>
      <Divider label="or" labelPosition="center" my="md" />
      <Button variant="default" fullWidth onClick={() => window.location.assign(ssoLoginUrl('/'))}>
        Sign in with SSO
      </Button>
      <Text size="xs" c="dimmed" ta="center" mt="md">
        New here? Ask your administrator for an invitation link.
      </Text>
    </AuthScreen>
  );
}

/** Centered card used by the sign-in, invitation and onboarding screens. */
export function AuthScreen({
  title,
  subtitle,
  width = 420,
  children,
}: {
  title: string;
  subtitle?: string;
  width?: number;
  children: ReactNode;
}) {
  return (
    <Center mih="100vh" p="md" className={classes.screen}>
      <Stack w="100%" maw={width} gap="lg">
        <Stack align="center" gap={6}>
          <ThemeIcon size={48} radius="md" variant="gradient" gradient={{ from: 'brand.7', to: 'cyan.5', deg: 135 }}>
            <IconStack2 size={28} />
          </ThemeIcon>
          <Title order={2}>{title}</Title>
          {subtitle && (
            <Text c="dimmed" size="sm" ta="center">
              {subtitle}
            </Text>
          )}
        </Stack>
        <Paper withBorder shadow="sm" p="xl">
          {children}
        </Paper>
      </Stack>
    </Center>
  );
}
