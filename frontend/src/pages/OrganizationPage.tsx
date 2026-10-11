import { Card, Group, Text } from '@mantine/core';
import { notifications } from '@mantine/notifications';
import * as api from '../api/client';
import { useAuth } from '../state/auth';
import { formatDateTime, shortCode } from '../lib/format';
import { PageHeader } from '../components/common';
import CustomerProfileForm from '../components/CustomerProfileForm';
import { toProfile } from '../lib/profile';

/** The customer's profile; editable by customer admins (except the short name). */
export default function OrganizationPage() {
  const { me, refresh } = useAuth();
  const customer = me?.customer;
  if (!customer) return null;

  return (
    <>
      <PageHeader
        title="Organization"
        description={
          <Group gap={6} component="span">
            <span>Short name</span>
            <Text span ff="monospace" fw={600}>
              {shortCode(customer.business_short_code)}
            </Text>
            <span>· onboarded {formatDateTime(customer.created_at)}</span>
          </Group>
        }
      />
      <Card p="xl">
        <CustomerProfileForm
          key={customer.id + customer.name}
          initial={toProfile(customer)}
          mode={me.is_customer_admin ? 'edit' : 'view'}
          submitLabel="Save changes"
          onSubmit={async (values) => {
            await api.updateMyCustomer(values);
            await refresh();
            notifications.show({ color: 'teal', message: 'Organization profile saved.' });
          }}
        />
      </Card>
    </>
  );
}
