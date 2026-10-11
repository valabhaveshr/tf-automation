import { useMemo, useState } from 'react';
import { Badge, Group, Loader, Select, Stack, Text, type ComboboxItem, type SelectProps } from '@mantine/core';
import { IconSearch } from '@tabler/icons-react';
import type { CatalogEntry } from '../api/types';
import { versionFromTag } from '../lib/schemaForm';
import { CloudBadge, ModuleIcon } from './common';

/**
 * Searchable module dropdown, grouped by category. Each option shows the
 * module's name, path, cloud provider and latest released version (the
 * version a new request pins). Modules already in the request are disabled.
 */
export default function ModulePicker({
  catalog,
  selected,
  loading,
  onPick,
}: {
  catalog: CatalogEntry[];
  selected: string[];
  loading?: boolean;
  onPick: (entry: CatalogEntry) => void;
}) {
  const [value, setValue] = useState<string | null>(null);
  const byName = useMemo(() => new Map(catalog.map((c) => [c.module_name, c])), [catalog]);

  const data = useMemo(() => {
    const groups = new Map<string, ComboboxItem[]>();
    for (const c of [...catalog].sort((a, b) => a.display_name.localeCompare(b.display_name))) {
      const group = c.category || 'Other';
      if (!groups.has(group)) groups.set(group, []);
      groups.get(group)!.push({
        value: c.module_name,
        label: `${c.display_name} (${c.module_name})`,
        disabled: selected.includes(c.module_name),
      });
    }
    return [...groups.entries()].sort(([a], [b]) => a.localeCompare(b)).map(([group, items]) => ({ group, items }));
  }, [catalog, selected]);

  const renderOption: SelectProps['renderOption'] = ({ option }) => {
    const c = byName.get(option.value);
    if (!c) return option.label;
    return (
      <Group gap="sm" wrap="nowrap" w="100%">
        <ModuleIcon icon={c.icon} category={c.category} size={30} />
        <Stack gap={0} style={{ flex: 1, minWidth: 0 }}>
          <Text size="sm" fw={500} truncate>
            {c.display_name}
          </Text>
          <Text size="xs" c="dimmed" ff="monospace" truncate>
            {c.module_name}
          </Text>
        </Stack>
        <Group gap={4} wrap="nowrap">
          <CloudBadge cloud={c.cloud} short />
          <Badge variant="outline" color="gray">
            {versionFromTag(c.latest_version_tag)}
          </Badge>
          {option.disabled && (
            <Badge variant="light" color="gray">
              added
            </Badge>
          )}
        </Group>
      </Group>
    );
  };

  return (
    <Select
      label="Add a module"
      description="Pick a module to add it to this request. You can add several modules, and several instances of each."
      placeholder={catalog.length ? 'Search by name, path or category…' : 'No modules in the catalog yet'}
      data={data}
      value={value}
      searchable
      nothingFoundMessage="No module matches"
      leftSection={loading ? <Loader size={16} /> : <IconSearch size={16} />}
      renderOption={renderOption}
      maxDropdownHeight={420}
      comboboxProps={{ shadow: 'md' }}
      disabled={loading || catalog.length === 0}
      onChange={(name) => {
        const entry = name ? byName.get(name) : undefined;
        if (entry) onPick(entry);
        setValue(null);
      }}
    />
  );
}
