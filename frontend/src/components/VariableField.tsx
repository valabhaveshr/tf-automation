import { useEffect, useState, type ReactNode } from 'react';
import {
  ActionIcon,
  Badge,
  Box,
  Button,
  Code,
  Group,
  HoverCard,
  JsonInput,
  List,
  NumberInput,
  PasswordInput,
  Select,
  Stack,
  Switch,
  TagsInput,
  Text,
  Textarea,
  TextInput,
  Tooltip,
} from '@mantine/core';
import { IconArrowBackUp, IconPlus, IconTrash } from '@tabler/icons-react';
import type { VariableHint, VarSchema } from '../api/types';
import { displayValue } from '../lib/format';
import { enumOptions, humanize, isMultiline, isStringList, isStringMap, parseExample } from '../lib/schemaForm';
import classes from './VariableField.module.css';

export interface VariableFieldProps {
  variable: VarSchema;
  hint?: VariableHint;
  /** The instance's own value; undefined means "not set" (the module default applies). */
  value: unknown;
  onChange: (value: unknown) => void;
  error?: string;
  /** Naming-engine suggestion pre-filled into the naming target field. */
  suggestedName?: string;
}

export default function VariableField({ variable: v, hint, value, onChange, error, suggestedName }: VariableFieldProps) {
  const overridden = value !== undefined;
  const showingDefault = !overridden && v.has_default;
  const effective = overridden ? value : suggestedName ?? (v.has_default ? v.default : undefined);
  const exampleValue = parseExample(v, hint?.example);

  const label = (
    <Group gap={6} wrap="nowrap" className={classes.label}>
      <Text component="span" size="sm" fw={500}>
        {humanize(v.name)}
      </Text>
      {v.required && (
        <Text component="span" c="red" size="sm" aria-hidden>
          *
        </Text>
      )}
      <InfoHover variable={v} />
      {hint?.example !== undefined && (
        <ExampleHover example={hint.example} canUse={exampleValue !== undefined} onUse={() => onChange(exampleValue)} />
      )}
      {showingDefault && (
        <Badge size="xs" variant="outline" color="gray">
          default
        </Badge>
      )}
      {suggestedName && !overridden && (
        <Badge size="xs" variant="light" color="grape">
          suggested
        </Badge>
      )}
      {v.sensitive && (
        <Badge size="xs" variant="light" color="orange">
          sensitive
        </Badge>
      )}
      {overridden && v.has_default && (
        <Tooltip label="Reset to the module default" withArrow>
          <ActionIcon size="xs" variant="subtle" color="gray" onClick={() => onChange(undefined)} aria-label="Reset to default">
            <IconArrowBackUp size={14} />
          </ActionIcon>
        </Tooltip>
      )}
    </Group>
  );

  const description = (
    <Text component="span" size="xs" c="dimmed" ff="monospace">
      {v.name}
    </Text>
  );

  return (
    <Box className={showingDefault ? classes.defaulted : undefined}>
      <FieldInput
        v={v}
        label={label}
        description={description}
        value={effective}
        placeholder={hint?.example ? `e.g. ${hint.example}` : undefined}
        error={error}
        onChange={onChange}
      />
    </Box>
  );
}

function InfoHover({ variable: v }: { variable: VarSchema }) {
  return (
    <HoverCard width={340} shadow="md" withArrow openDelay={120} position="top-start" withinPortal>
      <HoverCard.Target>
        <span className={classes.hintBadge} data-kind="info" aria-label={`About ${v.name}`} tabIndex={0}>
          i
        </span>
      </HoverCard.Target>
      <HoverCard.Dropdown>
        <Stack gap={8}>
          <Text size="sm" fw={600}>
            {humanize(v.name)}
          </Text>
          <Text size="sm">{v.description || 'No description provided by the module.'}</Text>
          <Group gap={6}>
            <Text size="xs" c="dimmed">
              Type
            </Text>
            <Code>{v.type_raw.replace(/\s+/g, ' ')}</Code>
            {v.required ? (
              <Badge size="xs" color="red" variant="light">
                required
              </Badge>
            ) : (
              <Badge size="xs" color="gray" variant="light">
                optional
              </Badge>
            )}
          </Group>
          {v.has_default && (
            <Group gap={6}>
              <Text size="xs" c="dimmed">
                Default
              </Text>
              <Code>{displayValue(v.default)}</Code>
            </Group>
          )}
          {v.validations.length > 0 && (
            <Box>
              <Text size="xs" c="dimmed" mb={2}>
                Rules
              </Text>
              <List size="xs" spacing={2}>
                {v.validations.map((val, i) => (
                  <List.Item key={i}>{val.error_message}</List.Item>
                ))}
              </List>
            </Box>
          )}
          {v.sensitive && (
            <Text size="xs" c="orange">
              Sensitive: this value is written to the generated .auto.tfvars file in plain text.
            </Text>
          )}
        </Stack>
      </HoverCard.Dropdown>
    </HoverCard>
  );
}

function ExampleHover({ example, canUse, onUse }: { example: string; canUse: boolean; onUse: () => void }) {
  return (
    <HoverCard width={320} shadow="md" withArrow openDelay={120} position="top-start" withinPortal>
      <HoverCard.Target>
        <span className={classes.hintBadge} data-kind="example" aria-label="Example value" tabIndex={0}>
          e
        </span>
      </HoverCard.Target>
      <HoverCard.Dropdown>
        <Stack gap={8}>
          <Text size="xs" c="dimmed">
            Example
          </Text>
          <Code block>{example}</Code>
          {canUse && (
            <Button size="compact-xs" variant="light" onClick={onUse}>
              Use this example
            </Button>
          )}
        </Stack>
      </HoverCard.Dropdown>
    </HoverCard>
  );
}

interface FieldInputProps {
  v: VarSchema;
  label: ReactNode;
  description: ReactNode;
  value: unknown;
  placeholder?: string;
  error?: string;
  onChange: (value: unknown) => void;
}

function FieldInput({ v, label, description, value, placeholder, error, onChange }: FieldInputProps) {
  const common = { label, description, error, inputWrapperOrder: ['label', 'input', 'description', 'error'] as ('label' | 'input' | 'description' | 'error')[] };

  if (v.type === 'bool') {
    return (
      <Stack gap={4}>
        {label}
        <Switch
          checked={value === true}
          onChange={(e) => onChange(e.currentTarget.checked)}
          label={value === true ? 'Enabled' : 'Disabled'}
          error={error}
        />
        {description}
      </Stack>
    );
  }

  if (v.type === 'number') {
    return (
      <NumberInput
        {...common}
        value={typeof value === 'number' ? value : ''}
        placeholder={placeholder}
        onChange={(n) => onChange(n === '' ? undefined : Number(n))}
        allowDecimal
      />
    );
  }

  if (v.type === 'string' || v.type === 'any') {
    const text = typeof value === 'string' ? value : value === undefined || value === null ? '' : String(value);
    const setText = (t: string) => onChange(t === '' ? undefined : t);
    const options = enumOptions(v);
    if (options) {
      return (
        <Select
          {...common}
          data={options}
          value={text || null}
          placeholder={placeholder ?? 'Select…'}
          clearable={!v.required}
          onChange={(o) => onChange(o ?? undefined)}
          allowDeselect={!v.required}
        />
      );
    }
    if (v.sensitive) {
      return <PasswordInput {...common} value={text} placeholder={placeholder} onChange={(e) => setText(e.currentTarget.value)} />;
    }
    if (isMultiline(v)) {
      return (
        <Textarea
          {...common}
          value={text}
          placeholder={placeholder}
          autosize
          minRows={3}
          maxRows={12}
          styles={{ input: { fontFamily: 'var(--mantine-font-family-monospace)' } }}
          onChange={(e) => setText(e.currentTarget.value)}
        />
      );
    }
    return <TextInput {...common} value={text} placeholder={placeholder} onChange={(e) => setText(e.currentTarget.value)} />;
  }

  if (isStringList(v)) {
    const items = Array.isArray(value) ? value.map(String) : [];
    return (
      <TagsInput
        {...common}
        value={items}
        placeholder={items.length ? undefined : 'Type a value and press Enter'}
        onChange={(list) => onChange(list)}
        clearable
        splitChars={[',']}
      />
    );
  }

  if (isStringMap(v)) {
    const obj = value && typeof value === 'object' && !Array.isArray(value) ? (value as Record<string, unknown>) : {};
    return <KeyValueEditor label={label} description={description} error={error} value={obj} onChange={onChange} />;
  }

  return <JsonField {...common} value={value} placeholder={placeholder} onChange={onChange} typeRaw={v.type_raw} />;
}

function KeyValueEditor({
  label,
  description,
  error,
  value,
  onChange,
}: {
  label: ReactNode;
  description: ReactNode;
  error?: string;
  value: Record<string, unknown>;
  onChange: (value: unknown) => void;
}) {
  // Local rows so a half-typed key (or a duplicate) doesn't drop data.
  const [rows, setRows] = useState<[string, string][]>(() => Object.entries(value).map(([k, val]) => [k, String(val)]));
  const serialized = JSON.stringify(value);
  useEffect(() => {
    const current = JSON.stringify(Object.fromEntries(rows.filter(([k]) => k.trim())));
    if (current !== serialized) {
      setRows(Object.entries(value).map(([k, val]) => [k, String(val)]));
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [serialized]);

  const commit = (next: [string, string][]) => {
    setRows(next);
    onChange(Object.fromEntries(next.filter(([k]) => k.trim()).map(([k, val]) => [k.trim(), val])));
  };

  return (
    <Stack gap={6}>
      {label}
      {rows.length === 0 && (
        <Text size="xs" c="dimmed">
          No entries.
        </Text>
      )}
      {rows.map(([k, val], i) => (
        <Group key={i} gap={6} wrap="nowrap">
          <TextInput
            size="xs"
            placeholder="key"
            value={k}
            onChange={(e) => commit(rows.map((r, j) => (j === i ? [e.currentTarget.value, r[1]] : r)))}
            style={{ flex: 1 }}
          />
          <TextInput
            size="xs"
            placeholder="value"
            value={val}
            onChange={(e) => commit(rows.map((r, j) => (j === i ? [r[0], e.currentTarget.value] : r)))}
            style={{ flex: 1 }}
          />
          <ActionIcon variant="subtle" color="red" onClick={() => commit(rows.filter((_, j) => j !== i))} aria-label="Remove entry">
            <IconTrash size={14} />
          </ActionIcon>
        </Group>
      ))}
      <Group gap="xs">
        <Button size="compact-xs" variant="light" leftSection={<IconPlus size={12} />} onClick={() => setRows([...rows, ['', '']])}>
          Add entry
        </Button>
      </Group>
      {description}
      {error && (
        <Text size="xs" c="red">
          {error}
        </Text>
      )}
    </Stack>
  );
}

function JsonField({
  value,
  onChange,
  typeRaw,
  ...rest
}: {
  label: ReactNode;
  description: ReactNode;
  error?: string;
  value: unknown;
  placeholder?: string;
  typeRaw: string;
  onChange: (value: unknown) => void;
}) {
  const [text, setText] = useState(() => (value === undefined ? '' : JSON.stringify(value, null, 2)));
  const [parseError, setParseError] = useState<string | null>(null);
  const serialized = value === undefined ? '' : JSON.stringify(value);
  useEffect(() => {
    try {
      if ((text.trim() === '' ? '' : JSON.stringify(JSON.parse(text))) === serialized) return;
    } catch {
      return; // keep the user's in-progress text
    }
    setText(value === undefined ? '' : JSON.stringify(value, null, 2));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [serialized]);

  return (
    <JsonInput
      {...rest}
      error={parseError ?? rest.error}
      value={text}
      autosize
      minRows={3}
      maxRows={14}
      formatOnBlur
      validationError="Not valid JSON"
      onChange={(t) => {
        setText(t);
        if (t.trim() === '') {
          setParseError(null);
          onChange(undefined);
          return;
        }
        try {
          onChange(JSON.parse(t));
          setParseError(null);
        } catch {
          setParseError(`Enter JSON matching ${typeRaw.replace(/\s+/g, ' ')}`);
        }
      }}
    />
  );
}
