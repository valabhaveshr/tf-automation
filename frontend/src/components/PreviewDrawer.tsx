import { useEffect, useState } from 'react';
import { Alert, Badge, Button, Drawer, Group, List, Stack, Tabs, Text } from '@mantine/core';
import { IconAlertTriangle, IconFileCode, IconGitCompare, IconInfoCircle } from '@tabler/icons-react';
import type { PreviewResult } from '../api/types';
import HclCode from './HclCode';

/** Orders generated files the way a reader expects: main.tf, variables.tf, then tfvars, then backend.tf. */
function fileOrder(name: string): number {
  if (name === 'main.tf') return 0;
  if (name === 'variables.tf') return 1;
  if (name.endsWith('.auto.tfvars')) return 2;
  if (name === 'backend.tf') return 3;
  return 4;
}

/**
 * Generated-code preview: one tab per file, plus a diff tab when editing.
 * Preview and save share one server code path, so what's shown here is
 * byte-for-byte what a save commits.
 */
export default function PreviewDrawer({
  opened,
  onClose,
  result,
  hasSensitiveVariables,
  outputPath,
  saving,
  onSave,
  saveLabel,
}: {
  opened: boolean;
  onClose: () => void;
  result: PreviewResult | null;
  hasSensitiveVariables: boolean;
  outputPath: string;
  saving?: boolean;
  onSave?: () => void;
  saveLabel?: string;
}) {
  const files = result ? Object.keys(result.files).sort((a, b) => fileOrder(a) - fileOrder(b) || a.localeCompare(b)) : [];
  const hasDiff = !!result?.diff;
  const [tab, setTab] = useState<string | null>(null);
  useEffect(() => {
    setTab(hasDiff ? '__diff' : files[0] ?? null);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [result]);

  return (
    <Drawer
      opened={opened}
      onClose={onClose}
      position="right"
      size="min(1000px, 100%)"
      title={
        <Group gap="xs">
          <IconFileCode size={20} />
          <Text fw={600}>Generated Terraform</Text>
        </Group>
      }
    >
      {result && (
        <Stack gap="md">
          <Text size="sm" c="dimmed">
            These files will be committed to{' '}
            <Text span ff="monospace" c="var(--mantine-color-text)">
              {outputPath || '<customer>/<project>/<environment>'}/
            </Text>{' '}
            in the output repository.
          </Text>

          {hasSensitiveVariables && (
            <Alert color="orange" icon={<IconAlertTriangle size={18} />} title="Sensitive values in plain text">
              Variables marked sensitive are still written into the generated <code>.auto.tfvars</code> files in plain
              text. Secrets-manager handling is planned for a later phase.
            </Alert>
          )}
          {result.warnings && result.warnings.length > 0 && (
            <Alert color="yellow" icon={<IconInfoCircle size={18} />} title="Warnings">
              <List size="sm">
                {result.warnings.map((w, i) => (
                  <List.Item key={i}>{w}</List.Item>
                ))}
              </List>
            </Alert>
          )}

          <Tabs value={tab} onChange={setTab} variant="pills" radius="md">
            <Tabs.List>
              {hasDiff && (
                <Tabs.Tab value="__diff" leftSection={<IconGitCompare size={14} />}>
                  Changes
                </Tabs.Tab>
              )}
              {files.map((f) => (
                <Tabs.Tab key={f} value={f} ff="monospace">
                  {f}
                </Tabs.Tab>
              ))}
            </Tabs.List>
            {hasDiff && (
              <Tabs.Panel value="__diff" pt="md">
                <HclCode code={result.diff ?? ''} diff maxHeight={640} />
              </Tabs.Panel>
            )}
            {result.diff === '' && (
              <Badge color="gray" variant="light" mt="sm">
                No changes compared to the committed version
              </Badge>
            )}
            {files.map((f) => (
              <Tabs.Panel key={f} value={f} pt="md">
                <HclCode code={result.files[f]} maxHeight={640} />
              </Tabs.Panel>
            ))}
          </Tabs>

          {onSave && (
            <Group justify="flex-end" mt="md">
              <Button variant="default" onClick={onClose}>
                Back to editing
              </Button>
              <Button onClick={onSave} loading={saving}>
                {saveLabel ?? 'Save'}
              </Button>
            </Group>
          )}
        </Stack>
      )}
    </Drawer>
  );
}
