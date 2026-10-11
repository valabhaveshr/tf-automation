import { useState, type FormEvent, type ReactNode } from 'react';
import {
  Alert,
  Autocomplete,
  Badge,
  Button,
  Divider,
  Group,
  SegmentedControl,
  Select,
  SimpleGrid,
  Stack,
  Text,
  TextInput,
} from '@mantine/core';
import { IconAlertCircle, IconLock } from '@tabler/icons-react';
import type { CustomerProfileInput } from '../api/types';
import { ApiError } from '../api/client';
import { errorMessage } from '../lib/format';
import { SHORT_CODE, suggestShortCode } from '../lib/profile';

const REGIONS: Record<string, string[]> = {
  gcp: [
    'me-central1 (Doha)',
    'me-central2 (Dammam)',
    'me-west1 (Tel Aviv)',
    'europe-west1 (Belgium)',
    'europe-west2 (London)',
    'europe-west3 (Frankfurt)',
    'us-central1 (Iowa)',
    'us-east1 (South Carolina)',
    'us-east4 (Virginia)',
    'us-west1 (Oregon)',
    'asia-south1 (Mumbai)',
    'asia-southeast1 (Singapore)',
    'australia-southeast1 (Sydney)',
  ],
  aws: ['me-central-1 (UAE)', 'me-south-1 (Bahrain)', 'eu-west-1 (Ireland)', 'eu-central-1 (Frankfurt)', 'us-east-1 (N. Virginia)', 'us-west-2 (Oregon)', 'ap-south-1 (Mumbai)'],
  azure: ['qatarcentral', 'uaenorth', 'westeurope', 'northeurope', 'eastus', 'westus2', 'centralindia'],
};

const INDUSTRIES = [
  'Sports & recreation',
  'Government & public sector',
  'Financial services',
  'Healthcare',
  'Education',
  'Energy & utilities',
  'Retail & e-commerce',
  'Manufacturing',
  'Media & entertainment',
  'Technology',
  'Telecommunications',
  'Transport & logistics',
  'Hospitality & travel',
  'Other',
];

const COUNTRIES = [
  'Qatar',
  'United Arab Emirates',
  'Saudi Arabia',
  'Kuwait',
  'Bahrain',
  'Oman',
  'India',
  'United Kingdom',
  'United States',
  'Germany',
  'Singapore',
];

/**
 * Organization profile form, shared by onboarding (everything editable) and
 * the Organization page (short name locked; read-only for non-admins).
 */
export default function CustomerProfileForm({
  initial,
  mode,
  submitLabel,
  onSubmit,
}: {
  initial: CustomerProfileInput;
  mode: 'onboarding' | 'edit' | 'view';
  submitLabel: string;
  onSubmit?: (values: CustomerProfileInput) => Promise<void>;
}) {
  const [values, setValues] = useState<CustomerProfileInput>(initial);
  const [shortCodeTouched, setShortCodeTouched] = useState(mode !== 'onboarding' || !!initial.business_short_code);
  const [errors, setErrors] = useState<Partial<Record<keyof CustomerProfileInput, string>>>({});
  const [formError, setFormError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const readOnly = mode === 'view';
  const shortCodeLocked = mode !== 'onboarding';

  const set = <K extends keyof CustomerProfileInput>(key: K, value: CustomerProfileInput[K]) => {
    setValues((prev) => {
      const next = { ...prev, [key]: value };
      if (key === 'name' && !shortCodeTouched) next.business_short_code = suggestShortCode(String(value));
      return next;
    });
    setErrors((prev) => ({ ...prev, [key]: undefined }));
  };

  function validate(): boolean {
    const next: typeof errors = {};
    if (!values.name.trim()) next.name = 'Organization name is required.';
    if (!shortCodeLocked && !SHORT_CODE.test(values.business_short_code.trim())) {
      next.business_short_code = '1-5 letters or digits, e.g. DMSC.';
    }
    if (values.contact_email && !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(values.contact_email)) {
      next.contact_email = 'Enter a valid email address.';
    }
    setErrors(next);
    return Object.keys(next).length === 0;
  }

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    if (!onSubmit || !validate()) return;
    setSubmitting(true);
    setFormError(null);
    try {
      await onSubmit({
        ...values,
        default_region: values.default_region.replace(/\s*\(.*\)$/, ''),
      });
    } catch (err) {
      const msg = errorMessage(err, 'Saving failed.');
      if (err instanceof ApiError && err.status === 409 && /short name/i.test(msg)) {
        setErrors((prev) => ({ ...prev, business_short_code: msg }));
      } else {
        setFormError(msg);
      }
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <form onSubmit={handleSubmit}>
      <Stack gap="lg">
        <Section title="Organization" description="How your organization appears across the portal.">
          <SimpleGrid cols={{ base: 1, sm: 2 }}>
            <TextInput
              label="Organization name"
              placeholder="Doha Marine Sports Centre"
              withAsterisk={!readOnly}
              value={values.name}
              onChange={(e) => set('name', e.currentTarget.value)}
              error={errors.name}
              readOnly={readOnly}
              data-autofocus
            />
            <TextInput
              label={
                <Group gap={6} component="span">
                  Unique short name
                  {shortCodeLocked && <IconLock size={13} />}
                </Group>
              }
              description={
                shortCodeLocked
                  ? 'Fixed after onboarding: it is part of generated resource names.'
                  : 'Up to 5 letters or digits. Used in resource names and must be unique.'
              }
              placeholder="DMSC"
              withAsterisk={!shortCodeLocked}
              value={values.business_short_code.toUpperCase()}
              onChange={(e) => {
                setShortCodeTouched(true);
                set('business_short_code', e.currentTarget.value.replace(/[^A-Za-z0-9]/g, '').slice(0, 5));
              }}
              error={errors.business_short_code}
              readOnly={shortCodeLocked}
              styles={{ input: { fontFamily: 'var(--mantine-font-family-monospace)', letterSpacing: '0.08em' } }}
            />
            <Select
              label="Industry"
              placeholder="Select an industry"
              data={INDUSTRIES}
              value={values.industry || null}
              onChange={(v) => set('industry', v ?? '')}
              searchable
              clearable={!readOnly}
              readOnly={readOnly}
            />
            <Autocomplete
              label="Country"
              placeholder="Qatar"
              data={COUNTRIES}
              value={values.country}
              onChange={(v) => set('country', v)}
              readOnly={readOnly}
            />
          </SimpleGrid>
        </Section>

        <Section title="Primary contact" description="Who we reach out to about this organization's infrastructure.">
          <SimpleGrid cols={{ base: 1, sm: 3 }}>
            <TextInput
              label="Full name"
              placeholder="Aisha Al-Thani"
              value={values.contact_name}
              onChange={(e) => set('contact_name', e.currentTarget.value)}
              readOnly={readOnly}
            />
            <TextInput
              label="Email"
              placeholder="it@dmsc.qa"
              type="email"
              value={values.contact_email}
              onChange={(e) => set('contact_email', e.currentTarget.value)}
              error={errors.contact_email}
              readOnly={readOnly}
            />
            <TextInput
              label="Phone"
              placeholder="+974 4400 0000"
              value={values.contact_phone}
              onChange={(e) => set('contact_phone', e.currentTarget.value)}
              readOnly={readOnly}
            />
          </SimpleGrid>
        </Section>

        <Section title="Cloud defaults" description="Pre-selected when your team requests resources. Optional.">
          <Stack gap="md">
            <div>
              <Text size="sm" fw={500} mb={4}>
                Default cloud provider
              </Text>
              <SegmentedControl
                value={values.default_cloud || 'gcp'}
                onChange={(v) => set('default_cloud', v)}
                data={[
                  { value: 'gcp', label: 'Google Cloud' },
                  { value: 'aws', label: 'AWS' },
                  { value: 'azure', label: 'Azure' },
                ]}
                readOnly={readOnly}
                disabled={readOnly}
              />
            </div>
            <SimpleGrid cols={{ base: 1, sm: 3 }}>
              <Autocomplete
                label="Default region"
                placeholder={values.default_cloud === 'aws' ? 'me-central-1' : values.default_cloud === 'azure' ? 'qatarcentral' : 'me-central1'}
                data={REGIONS[values.default_cloud || 'gcp'] ?? []}
                value={values.default_region}
                onChange={(v) => set('default_region', v)}
                readOnly={readOnly}
              />
              <TextInput
                label="GCP organization ID"
                placeholder="123456789012"
                value={values.gcp_organization_id}
                onChange={(e) => set('gcp_organization_id', e.currentTarget.value)}
                readOnly={readOnly}
              />
              <TextInput
                label="Billing account ID"
                placeholder="01ABCD-23EFGH-45IJKL"
                value={values.billing_account_id}
                onChange={(e) => set('billing_account_id', e.currentTarget.value)}
                readOnly={readOnly}
              />
            </SimpleGrid>
          </Stack>
        </Section>

        {formError && (
          <Alert color="red" icon={<IconAlertCircle size={18} />}>
            {formError}
          </Alert>
        )}

        {!readOnly && (
          <Group justify="flex-end">
            <Button type="submit" loading={submitting} size="md">
              {submitLabel}
            </Button>
          </Group>
        )}
        {readOnly && (
          <Badge variant="light" color="gray" w="fit-content">
            Only customer admins can edit the organization profile
          </Badge>
        )}
      </Stack>
    </form>
  );
}

function Section({ title, description, children }: { title: string; description: string; children: ReactNode }) {
  return (
    <Stack gap="sm">
      <div>
        <Text fw={600}>{title}</Text>
        <Text size="sm" c="dimmed">
          {description}
        </Text>
      </div>
      {children}
      <Divider mt="xs" />
    </Stack>
  );
}
