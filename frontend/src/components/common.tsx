import type { ReactNode } from 'react';
import { Badge, Box, Group, Stack, Text, ThemeIcon, Title, type MantineColor } from '@mantine/core';
import {
  IconBox,
  IconBucket,
  IconCloud,
  IconCloudComputing,
  IconDatabase,
  IconKey,
  IconLock,
  IconNetwork,
  IconServer,
  IconShieldLock,
  IconTopologyStar3,
  IconUsersGroup,
} from '@tabler/icons-react';
import type { CloudProvider, SyncStatus } from '../api/types';

export function PageHeader({
  title,
  description,
  actions,
}: {
  title: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
}) {
  return (
    <Group justify="space-between" align="flex-end" mb="lg" wrap="wrap" gap="sm">
      <Stack gap={4}>
        <Title order={2}>{title}</Title>
        {description && (
          <Text c="dimmed" size="sm">
            {description}
          </Text>
        )}
      </Stack>
      {actions && <Group gap="sm">{actions}</Group>}
    </Group>
  );
}

const STATUS: Record<SyncStatus, { color: MantineColor; label: string }> = {
  synced: { color: 'teal', label: 'Synced' },
  pending: { color: 'yellow', label: 'Pending' },
  failed: { color: 'red', label: 'Sync failed' },
};

export function StatusBadge({ status }: { status: SyncStatus }) {
  const s = STATUS[status] ?? { color: 'gray', label: status };
  return (
    <Badge color={s.color} variant="light">
      {s.label}
    </Badge>
  );
}

const CLOUDS: Record<string, { color: MantineColor; label: string }> = {
  gcp: { color: 'blue', label: 'Google Cloud' },
  aws: { color: 'orange', label: 'AWS' },
  azure: { color: 'cyan', label: 'Azure' },
  multi: { color: 'gray', label: 'Multi-cloud' },
};

export function CloudBadge({ cloud, short }: { cloud: CloudProvider | string; short?: boolean }) {
  const c = CLOUDS[cloud] ?? { color: 'gray', label: cloud };
  return (
    <Badge color={c.color} variant="light" leftSection={<IconCloud size={12} />}>
      {short ? cloud.toUpperCase() : c.label}
    </Badge>
  );
}

const ICONS: Record<string, typeof IconBox> = {
  server: IconServer,
  compute: IconServer,
  key: IconKey,
  iam: IconKey,
  database: IconDatabase,
  network: IconNetwork,
  networking: IconTopologyStar3,
  bucket: IconBucket,
  storage: IconBucket,
  lock: IconLock,
  kms: IconLock,
  shield: IconShieldLock,
  security: IconShieldLock,
  users: IconUsersGroup,
  cloud: IconCloudComputing,
};

export function ModuleIcon({
  icon,
  category,
  color = 'brand',
  size = 38,
}: {
  icon?: string;
  category?: string;
  color?: MantineColor;
  size?: number;
}) {
  const Icon = ICONS[icon ?? ''] ?? ICONS[(category ?? '').toLowerCase()] ?? IconBox;
  return (
    <ThemeIcon variant="light" color={color} size={size} radius="md">
      <Icon size={size * 0.55} stroke={1.6} />
    </ThemeIcon>
  );
}

export function EmptyState({
  icon,
  title,
  description,
  action,
}: {
  icon: ReactNode;
  title: string;
  description?: ReactNode;
  action?: ReactNode;
}) {
  return (
    <Stack align="center" gap="xs" py={48} px="md">
      <Box c="dimmed">{icon}</Box>
      <Text fw={600}>{title}</Text>
      {description && (
        <Text c="dimmed" size="sm" ta="center" maw={420}>
          {description}
        </Text>
      )}
      {action && <Box mt="sm">{action}</Box>}
    </Stack>
  );
}

const ROLE_LABELS: Record<string, { label: string; color: MantineColor }> = {
  admin: { label: 'Platform admin', color: 'red' },
  customer_admin: { label: 'Customer admin', color: 'grape' },
  requester: { label: 'Member', color: 'gray' },
  approver: { label: 'Approver', color: 'blue' },
};

/** Shows the most significant role only: admin > customer admin > member. */
export function RoleBadge({ roles }: { roles: string[] }) {
  const role = ['admin', 'customer_admin', 'approver', 'requester'].find((r) => roles.includes(r)) ?? 'requester';
  const r = ROLE_LABELS[role];
  return (
    <Badge color={r.color} variant="light">
      {r.label}
    </Badge>
  );
}
