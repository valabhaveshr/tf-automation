import { useCallback, useEffect, useMemo, useState } from 'react';
import { Alert, Badge, Button, Card, Group, SegmentedControl, SimpleGrid, Skeleton, Stack, Text, TextInput } from '@mantine/core';
import { notifications } from '@mantine/notifications';
import { IconPackages, IconRefresh, IconSearch } from '@tabler/icons-react';
import * as api from '../api/client';
import type { CatalogEntry } from '../api/types';
import { useAuth } from '../state/auth';
import { errorMessage } from '../lib/format';
import { versionFromTag } from '../lib/schemaForm';
import { CloudBadge, EmptyState, ModuleIcon, PageHeader } from '../components/common';

/** Browse every released module; platform admins can rescan the modules repo. */
export default function CatalogPage() {
  const { me } = useAuth();
  const [catalog, setCatalog] = useState<CatalogEntry[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [search, setSearch] = useState('');
  const [category, setCategory] = useState('all');
  const [refreshing, setRefreshing] = useState(false);

  const load = useCallback(async () => {
    try {
      setCatalog(await api.getCatalog());
    } catch (err) {
      setError(errorMessage(err, 'Could not load the catalog.'));
      setCatalog([]);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  async function refresh() {
    setRefreshing(true);
    try {
      await api.refreshCatalog();
      await load();
      notifications.show({ color: 'teal', message: 'Catalog refreshed from the modules repository.' });
    } catch (err) {
      notifications.show({ color: 'red', title: 'Refresh failed', message: errorMessage(err, 'Refresh failed.') });
    } finally {
      setRefreshing(false);
    }
  }

  const categories = useMemo(() => [...new Set((catalog ?? []).map((c) => c.category || 'Other'))].sort(), [catalog]);
  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    return (catalog ?? []).filter(
      (c) =>
        (category === 'all' || (c.category || 'Other') === category) &&
        (!q || [c.display_name, c.module_name, c.description, c.category].some((s) => s.toLowerCase().includes(q))),
    );
  }, [catalog, search, category]);

  return (
    <>
      <PageHeader
        title="Module catalog"
        description="Released Terraform modules you can add to a resource request. Only tagged versions appear here."
        actions={
          me?.is_platform_admin && (
            <Button variant="default" leftSection={<IconRefresh size={18} />} loading={refreshing} onClick={refresh}>
              Refresh catalog
            </Button>
          )
        }
      />
      {error && (
        <Alert color="red" mb="md">
          {error}
        </Alert>
      )}
      <Group mb="md" gap="sm" wrap="wrap">
        <TextInput
          placeholder="Search modules…"
          leftSection={<IconSearch size={16} />}
          value={search}
          onChange={(e) => setSearch(e.currentTarget.value)}
          style={{ flex: 1, minWidth: 220 }}
        />
        {categories.length > 1 && (
          <SegmentedControl value={category} onChange={setCategory} data={[{ value: 'all', label: 'All' }, ...categories.map((c) => ({ value: c, label: c }))]} />
        )}
      </Group>
      {!catalog ? (
        <SimpleGrid cols={{ base: 1, sm: 2, lg: 3 }}>
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} h={150} radius="lg" />
          ))}
        </SimpleGrid>
      ) : filtered.length === 0 ? (
        <Card>
          <EmptyState
            icon={<IconPackages size={44} stroke={1.3} />}
            title={catalog.length ? 'No module matches' : 'The catalog is empty'}
            description={
              catalog.length
                ? undefined
                : 'Modules appear once they are tagged in the modules repository (e.g. modules/compute/instance/v1.0.0). Platform admins can refresh the catalog.'
            }
          />
        </Card>
      ) : (
        <SimpleGrid cols={{ base: 1, sm: 2, lg: 3 }}>
          {filtered.map((c) => (
            <Card key={c.module_name} p="lg">
              <Stack gap="sm" h="100%">
                <Group justify="space-between" align="flex-start" wrap="nowrap">
                  <Group gap="sm" wrap="nowrap" style={{ minWidth: 0 }}>
                    <ModuleIcon icon={c.icon} category={c.category} />
                    <div style={{ minWidth: 0 }}>
                      <Text fw={600} truncate>
                        {c.display_name}
                      </Text>
                      <Text size="xs" c="dimmed" ff="monospace" truncate>
                        {c.module_name}
                      </Text>
                    </div>
                  </Group>
                  <Badge variant="outline" color="gray">
                    {versionFromTag(c.latest_version_tag)}
                  </Badge>
                </Group>
                <Text size="sm" c="dimmed" style={{ flex: 1 }}>
                  {c.description}
                </Text>
                <Group gap={6}>
                  <CloudBadge cloud={c.cloud} />
                  {c.category && (
                    <Badge variant="dot" color="gray">
                      {c.category}
                    </Badge>
                  )}
                </Group>
              </Stack>
            </Card>
          ))}
        </SimpleGrid>
      )}
    </>
  );
}
