import type { ReactNode } from 'react';
import {
  ActionIcon,
  AppShell,
  Avatar,
  Badge,
  Box,
  Burger,
  Group,
  Menu,
  NavLink,
  ScrollArea,
  Stack,
  Text,
  ThemeIcon,
  Tooltip,
  UnstyledButton,
  useComputedColorScheme,
  useMantineColorScheme,
} from '@mantine/core';
import { useDisclosure } from '@mantine/hooks';
import {
  IconBuildingSkyscraper,
  IconBuildingStore,
  IconCirclePlus,
  IconHistory,
  IconLayoutDashboard,
  IconLogout,
  IconMoon,
  IconPackages,
  IconStack2,
  IconSun,
  IconUsers,
} from '@tabler/icons-react';
import { Link, useLocation, useNavigate } from 'react-router-dom';
import { useAuth } from '../state/auth';
import { shortCode } from '../lib/format';
import { RoleBadge } from '../components/common';

interface NavItem {
  to: string;
  label: string;
  icon: typeof IconStack2;
  /** Also active on these path prefixes. */
  match?: string[];
}

export default function AppLayout({ children }: { children: ReactNode }) {
  const [opened, { toggle, close }] = useDisclosure();
  const { me, logout } = useAuth();
  const location = useLocation();
  const navigate = useNavigate();
  const { setColorScheme } = useMantineColorScheme();
  const scheme = useComputedColorScheme('light');

  if (!me) return null;
  const platform = me.is_platform_admin;

  const main: NavItem[] = [
    { to: '/', label: 'Resources', icon: IconLayoutDashboard, match: ['/resources/'] },
    { to: '/resources/new', label: 'New resource', icon: IconCirclePlus },
    { to: '/catalog', label: 'Module catalog', icon: IconPackages },
  ];
  const org: NavItem[] = platform
    ? [
        { to: '/admin/customers', label: 'Customers', icon: IconBuildingStore },
        { to: '/admin/audit-log', label: 'Audit log', icon: IconHistory },
      ]
    : [
        { to: '/team', label: 'Team', icon: IconUsers },
        { to: '/organization', label: 'Organization', icon: IconBuildingSkyscraper },
      ];

  const isActive = (item: NavItem) => {
    if (item.to === '/') {
      return location.pathname === '/' || (location.pathname.startsWith('/resources/') && location.pathname !== '/resources/new');
    }
    return location.pathname === item.to || location.pathname.startsWith(item.to + '/');
  };

  const renderNav = (items: NavItem[]) =>
    items.map((item) => (
      <NavLink
        key={item.to}
        component={Link}
        to={item.to}
        label={item.label}
        leftSection={<item.icon size={18} stroke={1.7} />}
        active={isActive(item)}
        onClick={close}
        variant="light"
        style={{ borderRadius: 'var(--mantine-radius-md)' }}
      />
    ));

  return (
    <AppShell
      header={{ height: 60 }}
      navbar={{ width: 250, breakpoint: 'sm', collapsed: { mobile: !opened } }}
      padding="lg"
    >
      <AppShell.Header>
        <Group h="100%" px="md" justify="space-between" wrap="nowrap">
          <Group gap="sm" wrap="nowrap">
            <Burger opened={opened} onClick={toggle} hiddenFrom="sm" size="sm" />
            <UnstyledButton component={Link} to="/">
              <Group gap={8} wrap="nowrap">
                <ThemeIcon size={32} radius="md" variant="gradient" gradient={{ from: 'brand.7', to: 'cyan.5', deg: 135 }}>
                  <IconStack2 size={19} />
                </ThemeIcon>
                <Box visibleFrom="xs">
                  <Text fw={700} lh={1.1}>
                    Infra Portal
                  </Text>
                  <Text size="xs" c="dimmed" lh={1.1}>
                    Self-service Terraform
                  </Text>
                </Box>
              </Group>
            </UnstyledButton>
          </Group>

          <Group gap="sm" wrap="nowrap">
            {me.customer ? (
              <Group gap={6} wrap="nowrap" visibleFrom="sm">
                <Text size="sm" fw={500} truncate maw={260}>
                  {me.customer.name}
                </Text>
                <Badge variant="light" size="md">
                  {shortCode(me.customer.business_short_code)}
                </Badge>
              </Group>
            ) : (
              platform && (
                <Badge color="red" variant="light" visibleFrom="sm">
                  Platform admin
                </Badge>
              )
            )}
            <Tooltip label={scheme === 'dark' ? 'Light mode' : 'Dark mode'} withArrow>
              <ActionIcon
                variant="default"
                size="lg"
                onClick={() => setColorScheme(scheme === 'dark' ? 'light' : 'dark')}
                aria-label="Toggle color scheme"
              >
                {scheme === 'dark' ? <IconSun size={18} /> : <IconMoon size={18} />}
              </ActionIcon>
            </Tooltip>
            <Menu position="bottom-end" withArrow shadow="md" width={240}>
              <Menu.Target>
                <UnstyledButton aria-label="Account menu">
                  <Avatar color="brand" radius="xl" size={36}>
                    {me.user.username.slice(0, 2).toUpperCase()}
                  </Avatar>
                </UnstyledButton>
              </Menu.Target>
              <Menu.Dropdown>
                <Box px="sm" py={6}>
                  <Text size="sm" fw={600} truncate>
                    {me.user.username}
                  </Text>
                  <Box mt={4}>
                    <RoleBadge roles={me.user.roles} />
                  </Box>
                </Box>
                <Menu.Divider />
                <Menu.Item
                  color="red"
                  leftSection={<IconLogout size={16} />}
                  onClick={async () => {
                    await logout();
                    navigate('/login');
                  }}
                >
                  Sign out
                </Menu.Item>
              </Menu.Dropdown>
            </Menu>
          </Group>
        </Group>
      </AppShell.Header>

      <AppShell.Navbar p="sm">
        <AppShell.Section grow component={ScrollArea}>
          <Stack gap={4}>
            <Text size="xs" fw={700} c="dimmed" tt="uppercase" px="sm" pt="xs" pb={4}>
              Infrastructure
            </Text>
            {renderNav(main)}
            <Text size="xs" fw={700} c="dimmed" tt="uppercase" px="sm" pt="md" pb={4}>
              {platform ? 'Administration' : 'Organization'}
            </Text>
            {renderNav(org)}
          </Stack>
        </AppShell.Section>
        <AppShell.Section>
          <Text size="xs" c="dimmed" px="sm" pb={4}>
            Generated code is committed to Git for review before any apply.
          </Text>
        </AppShell.Section>
      </AppShell.Navbar>

      <AppShell.Main>
        <Box maw={1400} mx="auto">
          {children}
        </Box>
      </AppShell.Main>
    </AppShell>
  );
}
