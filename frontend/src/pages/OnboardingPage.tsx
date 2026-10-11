import { Badge, Group, Stack, Text } from '@mantine/core';
import { notifications } from '@mantine/notifications';
import { Navigate, useNavigate } from 'react-router-dom';
import * as api from '../api/client';
import { useAuth } from '../state/auth';
import CustomerProfileForm from '../components/CustomerProfileForm';
import { EMPTY_PROFILE } from '../lib/profile';
import { AuthScreen } from './LoginPage';

/** First sign-in of an invited customer admin: set up the organization. */
export default function OnboardingPage() {
  const { me, refresh } = useAuth();
  const navigate = useNavigate();

  if (me && !me.needs_onboarding) return <Navigate to="/" replace />;

  return (
    <AuthScreen
      title="Set up your organization"
      subtitle="A few details about your organization before you start requesting infrastructure."
      width={860}
    >
      <Stack gap="lg">
        <Group gap="xs">
          <Badge variant="light">Step 1 of 1</Badge>
          <Text size="sm" c="dimmed">
            You can change everything except the short name later, from the Organization page.
          </Text>
        </Group>
        <CustomerProfileForm
          initial={{ ...EMPTY_PROFILE, contact_email: me?.user.username.includes('@') ? me.user.username : '' }}
          mode="onboarding"
          submitLabel="Finish setup"
          onSubmit={async (values) => {
            const customer = await api.completeOnboarding(values);
            await refresh();
            notifications.show({ color: 'teal', title: 'Welcome aboard', message: `${customer.name} is ready.` });
            navigate('/', { replace: true });
          }}
        />
      </Stack>
    </AuthScreen>
  );
}
