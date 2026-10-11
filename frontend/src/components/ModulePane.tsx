import { useEffect, useMemo, useState } from 'react';
import {
  Accordion,
  ActionIcon,
  Badge,
  Box,
  Button,
  Card,
  Collapse,
  Group,
  Indicator,
  Menu,
  SimpleGrid,
  Stack,
  Tabs,
  Text,
  TextInput,
  Title,
  Tooltip,
  type MantineColor,
} from '@mantine/core';
import { modals } from '@mantine/modals';
import {
  IconChevronDown,
  IconCopy,
  IconDots,
  IconPlus,
  IconSettings,
  IconTrash,
} from '@tabler/icons-react';
import type { ModuleSchema } from '../api/types';
import { useRecordDraft, type DraftModuleSelection } from '../state/recordDraft';
import {
  groupVariables,
  hintFor,
  INSTANCE_NAME_PATTERN,
  instanceLabel,
  isWideField,
  nextInstanceName,
  validateInstances,
  versionFromTag,
  type VariableSection,
} from '../lib/schemaForm';
import VariableField from './VariableField';
import { CloudBadge, ModuleIcon } from './common';
import classes from './ModulePane.module.css';

function leafName(moduleName: string) {
  return moduleName.split('/').pop() ?? moduleName;
}

function namingTargetVariable(schema: ModuleSchema): string | null {
  const meta = schema.metadata;
  if (!meta) return null;
  if (meta.naming_target_variable) return meta.naming_target_variable;
  if (meta.naming_resource_type && meta.instance_label_field) return meta.instance_label_field;
  return null;
}

/**
 * One selected module in the resource editor: a card with a colored accent
 * (so each module's inputs are visually separate), its instances as tabs,
 * and each instance's fields grouped into sections from metadata.yaml.
 */
export default function ModulePane({
  module,
  accent,
  showErrors,
}: {
  module: DraftModuleSelection;
  accent: MantineColor;
  showErrors: boolean;
}) {
  const { draft, addInstance, removeInstance, renameInstance, duplicateInstance, removeModule } = useRecordDraft();
  const { schema, instances } = module;
  const names = Object.keys(instances);
  const [active, setActive] = useState<string | null>(names[0] ?? null);
  const [open, setOpen] = useState(true);
  const errors = useMemo(() => validateInstances(schema, instances), [schema, instances]);

  // Keep a valid tab selected as instances are added, renamed and removed.
  useEffect(() => {
    if (!active || !names.includes(active)) setActive(names[names.length - 1] ?? null);
  }, [names.join('|')]); // eslint-disable-line react-hooks/exhaustive-deps

  const meta = schema.metadata;
  const title = meta?.display_name || schema.module_name;

  function add() {
    const name = nextInstanceName(leafName(schema.module_name), names);
    addInstance(schema.module_name, name);
    setActive(name);
    setOpen(true);
  }

  function duplicate(source: string) {
    const name = nextInstanceName(source.replace(/-\d+$/, ''), names);
    duplicateInstance(schema.module_name, source, name);
    setActive(name);
  }

  function confirmRemoveModule() {
    modals.openConfirmModal({
      title: `Remove ${title}?`,
      children: (
        <Text size="sm">
          This removes the module and its {names.length} instance{names.length === 1 ? '' : 's'} from this request.
        </Text>
      ),
      labels: { confirm: 'Remove module', cancel: 'Cancel' },
      confirmProps: { color: 'red' },
      onConfirm: () => removeModule(schema.module_name),
    });
  }

  const errorCount = errors.length;

  return (
    <Card className={classes.pane} style={{ borderLeftColor: `var(--mantine-color-${accent}-6)` }} p={0}>
      <Group className={classes.header} justify="space-between" wrap="nowrap" p="md" onClick={() => setOpen((o) => !o)}>
        <Group gap="sm" wrap="nowrap" style={{ minWidth: 0 }}>
          <ModuleIcon icon={meta?.icon} category={meta?.category} color={accent} />
          <Box style={{ minWidth: 0 }}>
            <Group gap={8} wrap="wrap">
              <Title order={4} lh={1.2}>
                {title}
              </Title>
              {meta?.cloud && <CloudBadge cloud={meta.cloud} short />}
              <Badge variant="outline" color="gray">
                {versionFromTag(module.pinned_version ?? schema.version_tag)}
              </Badge>
              {meta?.category && (
                <Badge variant="dot" color={accent}>
                  {meta.category}
                </Badge>
              )}
            </Group>
            <Text size="xs" c="dimmed" ff="monospace" truncate>
              {schema.module_name}
            </Text>
          </Box>
        </Group>
        <Group gap="xs" wrap="nowrap" onClick={(e) => e.stopPropagation()}>
          <Badge variant="light" color={accent} size="lg">
            {names.length} instance{names.length === 1 ? '' : 's'}
          </Badge>
          {showErrors && errorCount > 0 && (
            <Badge variant="filled" color="red" size="lg">
              {errorCount} to fix
            </Badge>
          )}
          <Tooltip label="Remove module" withArrow>
            <ActionIcon variant="subtle" color="red" onClick={confirmRemoveModule} aria-label="Remove module">
              <IconTrash size={18} />
            </ActionIcon>
          </Tooltip>
          <ActionIcon variant="subtle" color="gray" onClick={() => setOpen((o) => !o)} aria-label={open ? 'Collapse' : 'Expand'}>
            <IconChevronDown size={18} className={classes.chevron} data-open={open || undefined} />
          </ActionIcon>
        </Group>
      </Group>

      <Collapse in={open}>
        <Box px="md" pb="md">
          {meta?.description && (
            <Text size="sm" c="dimmed" mb="sm">
              {meta.description}
            </Text>
          )}

          {names.length === 0 ? (
            <Stack align="center" gap="xs" py="lg" className={classes.emptyInstances}>
              <Text size="sm" c="dimmed">
                No instances yet. Each instance becomes one {title.toLowerCase()}.
              </Text>
              <Button leftSection={<IconPlus size={16} />} onClick={add} variant="light" color={accent}>
                Add instance
              </Button>
            </Stack>
          ) : (
            <Tabs value={active} onChange={setActive} variant="outline" keepMounted={false} color={accent}>
              <Group gap={0} wrap="nowrap" align="flex-end" className={classes.tabBar}>
                <Tabs.List style={{ flex: 1, flexWrap: 'wrap' }}>
                  {names.map((name) => {
                    const hasErrors = showErrors && errors.some((e) => e.instanceName === name);
                    const label = instanceLabel(schema, instances[name]);
                    return (
                      <Tabs.Tab key={name} value={name}>
                        <Indicator color="red" size={7} disabled={!hasErrors} offset={-2}>
                          <Group gap={6} wrap="nowrap">
                            <Text size="sm" fw={500} ff="monospace">
                              {name}
                            </Text>
                            {label && label !== name && (
                              <Text size="xs" c="dimmed" truncate maw={140}>
                                {label}
                              </Text>
                            )}
                          </Group>
                        </Indicator>
                      </Tabs.Tab>
                    );
                  })}
                </Tabs.List>
                <Button size="compact-sm" variant="subtle" color={accent} leftSection={<IconPlus size={14} />} onClick={add} mb={4} ml="xs">
                  Add instance
                </Button>
              </Group>

              {names.map((name) => (
                <Tabs.Panel key={name} value={name} pt="md">
                  <InstanceEditor
                    module={module}
                    instanceName={name}
                    takenNames={names}
                    accent={accent}
                    showErrors={showErrors}
                    suggestedName={draft.suggestedNames[`${schema.module_name}.${name}`]}
                    onRename={(next) => {
                      renameInstance(schema.module_name, name, next);
                      setActive(next);
                    }}
                    onDuplicate={() => duplicate(name)}
                    onRemove={() => removeInstance(schema.module_name, name)}
                  />
                </Tabs.Panel>
              ))}
            </Tabs>
          )}
        </Box>
      </Collapse>
    </Card>
  );
}

function InstanceEditor({
  module,
  instanceName,
  takenNames,
  accent,
  showErrors,
  suggestedName,
  onRename,
  onDuplicate,
  onRemove,
}: {
  module: DraftModuleSelection;
  instanceName: string;
  takenNames: string[];
  accent: MantineColor;
  showErrors: boolean;
  suggestedName?: string;
  onRename: (next: string) => void;
  onDuplicate: () => void;
  onRemove: () => void;
}) {
  const { updateInstanceField } = useRecordDraft();
  const { schema } = module;
  const stored = module.instances[instanceName];
  const values = useMemo(() => stored ?? {}, [stored]);
  const grouped = useMemo(() => groupVariables(schema), [schema]);
  const errors = useMemo(
    () => validateInstances(schema, { [instanceName]: values }),
    [schema, instanceName, values],
  );
  const target = namingTargetVariable(schema);

  const [nameDraft, setNameDraft] = useState(instanceName);
  const [nameError, setNameError] = useState<string | null>(null);
  useEffect(() => setNameDraft(instanceName), [instanceName]);

  function commitName() {
    const next = nameDraft.trim();
    if (next === instanceName) return setNameError(null);
    if (!INSTANCE_NAME_PATTERN.test(next)) {
      return setNameError('Lowercase letters, digits and hyphens only (max 63).');
    }
    if (takenNames.includes(next)) {
      return setNameError(`"${next}" is already used in this module.`);
    }
    setNameError(null);
    onRename(next);
  }

  const advancedCount = grouped.advanced.reduce((n, s) => n + s.fields.length, 0);

  const renderSection = (section: VariableSection) => (
    <Box key={section.title} className={classes.section}>
      <Text className={classes.sectionTitle} c={`${accent}.7`}>
        {section.title}
      </Text>
      <SimpleGrid cols={{ base: 1, md: 2 }} spacing="md" verticalSpacing="md">
        {section.fields.map((v) => (
          <Box key={v.name} style={isWideField(v) ? { gridColumn: '1 / -1' } : undefined}>
            <VariableField
              variable={v}
              hint={hintFor(schema, v.name)}
              value={values[v.name]}
              suggestedName={v.name === target ? suggestedName : undefined}
              error={showErrors ? errors.find((e) => e.varName === v.name)?.message : undefined}
              onChange={(val) => updateInstanceField(schema.module_name, instanceName, v.name, val)}
            />
          </Box>
        ))}
      </SimpleGrid>
    </Box>
  );

  return (
    <Stack gap="lg">
      <Group justify="space-between" align="flex-start" wrap="nowrap" className={classes.instanceHeader}>
        <TextInput
          label="Instance key"
          description="Unique within this module; becomes the for_each key in the generated code."
          value={nameDraft}
          onChange={(e) => setNameDraft(e.currentTarget.value)}
          onBlur={commitName}
          onKeyDown={(e) => e.key === 'Enter' && commitName()}
          error={nameError}
          styles={{ input: { fontFamily: 'var(--mantine-font-family-monospace)' } }}
          maw={360}
          style={{ flex: 1 }}
        />
        <Menu position="bottom-end" withArrow shadow="md">
          <Menu.Target>
            <Button variant="default" rightSection={<IconDots size={16} />} mt={22}>
              Instance actions
            </Button>
          </Menu.Target>
          <Menu.Dropdown>
            <Menu.Item leftSection={<IconCopy size={16} />} onClick={onDuplicate}>
              Duplicate instance
            </Menu.Item>
            <Menu.Divider />
            <Menu.Item color="red" leftSection={<IconTrash size={16} />} onClick={onRemove}>
              Remove instance
            </Menu.Item>
          </Menu.Dropdown>
        </Menu>
      </Group>

      {grouped.sections.map(renderSection)}

      {advancedCount > 0 && (
        <Accordion variant="contained" radius="md" chevronPosition="left">
          <Accordion.Item value="advanced">
            <Accordion.Control icon={<IconSettings size={16} />}>
              <Group gap={8}>
                <Text fw={500}>Advanced settings</Text>
                <Badge size="sm" variant="light" color="gray">
                  {advancedCount}
                </Badge>
                {showErrors && grouped.advanced.some((s) => s.fields.some((f) => errors.some((e) => e.varName === f.name))) && (
                  <Badge size="sm" color="red">
                    needs attention
                  </Badge>
                )}
              </Group>
            </Accordion.Control>
            <Accordion.Panel>
              <Stack gap="lg">{grouped.advanced.map(renderSection)}</Stack>
            </Accordion.Panel>
          </Accordion.Item>
        </Accordion>
      )}
    </Stack>
  );
}
