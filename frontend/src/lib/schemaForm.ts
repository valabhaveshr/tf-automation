/**
 * Pure helpers behind the resource form: validation, grouping variables into
 * sections from metadata.yaml hints, labels, enum detection, examples.
 *
 * Validation here is client-side convenience only (fast feedback, fewer round
 * trips). POST/PUT /api/records/preview is the authoritative validator per
 * plan/ARCHITECTURE.md — this must never be trusted as the sole gate.
 */
import type { InstanceValues, ModuleSchema, VariableHint, VarSchema } from '../api/types';

export const INSTANCE_NAME_PATTERN = /^[a-z0-9][a-z0-9-]{0,62}$/;

export interface InstanceValidationError {
  instanceName: string;
  varName?: string;
  message: string;
}

export function isEmptyValue(v: unknown): boolean {
  return v === undefined || v === null || v === '';
}

export function validateInstances(
  schema: ModuleSchema,
  instances: Record<string, InstanceValues>,
): InstanceValidationError[] {
  const errors: InstanceValidationError[] = [];
  const names = Object.keys(instances);
  const label = schema.metadata?.display_name ?? schema.module_name;

  if (names.length === 0) {
    errors.push({ instanceName: '', message: `${label}: add at least one instance.` });
  }

  for (const name of names) {
    if (!INSTANCE_NAME_PATTERN.test(name)) {
      errors.push({
        instanceName: name,
        message: `Instance name "${name}" must be lowercase letters, digits and hyphens (max 63), starting with a letter or digit.`,
      });
    }
  }

  for (const [instanceName, values] of Object.entries(instances)) {
    for (const v of schema.variables) {
      if (v.required && isEmptyValue(values[v.name])) {
        errors.push({ instanceName, varName: v.name, message: `${humanize(v.name)} is required.` });
      }
    }
  }

  return errors;
}

/** "boot_disk_size_gb" -> "Boot disk size gb", with a few common acronyms kept upper-case. */
export function humanize(name: string): string {
  const acronyms: Record<string, string> = {
    id: 'ID', ip: 'IP', gb: 'GB', kms: 'KMS', vtpm: 'vTPM', iam: 'IAM', url: 'URL', dns: 'DNS', vpc: 'VPC', ssh: 'SSH',
  };
  const words = name.split('_').filter(Boolean).map((w) => acronyms[w] ?? w);
  if (words.length === 0) return name;
  const [first, ...rest] = words;
  return [first === first.toLowerCase() ? first.charAt(0).toUpperCase() + first.slice(1) : first, ...rest].join(' ');
}

export function hintFor(schema: ModuleSchema, varName: string): VariableHint | undefined {
  return schema.metadata?.variables?.find((h) => h.name === varName);
}

export interface VariableSection {
  title: string;
  fields: VarSchema[];
}

export interface GroupedVariables {
  sections: VariableSection[];
  /** Hinted `advanced: true` fields, shown collapsed. Required fields are never hidden here. */
  advanced: VariableSection[];
}

/**
 * Orders variables by metadata.yaml's `variables:` list and buckets them by
 * `group`. Variables with no hint come last under "Other settings". A
 * required variable is never collapsed under Advanced, whatever its hint
 * says, so a user can't miss it.
 */
export function groupVariables(schema: ModuleSchema): GroupedVariables {
  const hints = schema.metadata?.variables ?? [];
  const order = new Map(hints.map((h, i) => [h.name, i]));
  const vars = [...schema.variables].sort(
    (a, b) => (order.get(a.name) ?? Number.MAX_SAFE_INTEGER) - (order.get(b.name) ?? Number.MAX_SAFE_INTEGER),
  );

  const sections: VariableSection[] = [];
  const advanced: VariableSection[] = [];
  const push = (list: VariableSection[], title: string, v: VarSchema) => {
    let section = list.find((s) => s.title === title);
    if (!section) {
      section = { title, fields: [] };
      list.push(section);
    }
    section.fields.push(v);
  };

  for (const v of vars) {
    const hint = hints.find((h) => h.name === v.name);
    const title = hint?.group || (hints.length > 0 ? 'Other settings' : 'Settings');
    if (hint?.advanced && !v.required) {
      push(advanced, title, v);
    } else {
      push(sections, title, v);
    }
  }
  return { sections, advanced };
}

/**
 * Detects a fixed set of allowed values from a validation like
 * `contains(["STANDARD", "SPOT"], var.provisioning_model)`, so the field can
 * render as a dropdown. Returns null when there's no such validation.
 */
export function enumOptions(v: VarSchema): string[] | null {
  if (v.type !== 'string') return null;
  const re = new RegExp(String.raw`contains\(\s*\[([^\]]*)\]\s*,\s*var\.${v.name}\s*\)`);
  for (const val of v.validations ?? []) {
    const m = re.exec(val.condition_raw);
    if (!m) continue;
    const opts = [...m[1].matchAll(/"((?:[^"\\]|\\.)*)"/g)].map((x) => x[1]);
    if (opts.length > 0) return opts;
  }
  return null;
}

/** Instance's display label: the module's instance_label_field value, if set. */
export function instanceLabel(schema: ModuleSchema, values: InstanceValues | undefined): string | null {
  const field = schema.metadata?.instance_label_field;
  if (!field || !values) return null;
  const v = values[field];
  return typeof v === 'string' && v.trim() ? v : null;
}

/** "modules/compute/instance/v1.2.0" -> "v1.2.0" */
export function versionFromTag(tag: string | null | undefined): string {
  if (!tag) return '';
  const parts = tag.split('/');
  return parts[parts.length - 1] ?? tag;
}

/**
 * Converts a metadata.yaml example (display text) into a value the field can
 * hold, for the "Use example" action. Returns undefined when the example
 * can't be mapped onto the field's type safely; the hover still shows it.
 */
export function parseExample(v: VarSchema, example: string | undefined): unknown {
  if (example === undefined) return undefined;
  const text = example.trim();
  switch (v.type) {
    case 'string':
      return text.replace(/^"(.*)"$/s, '$1');
    case 'number': {
      const n = Number(text);
      return Number.isFinite(n) ? n : undefined;
    }
    case 'bool':
      return text === 'true' ? true : text === 'false' ? false : undefined;
    case 'list':
    case 'set':
    case 'map':
    case 'object':
      return parseHclLiteral(text);
    default:
      return undefined;
  }
}

/** Parses a simple HCL/JSON literal (`["a", "b"]`, `{ team = "x" }`) as JSON. */
function parseHclLiteral(text: string): unknown {
  const asJson = text
    // `key = value` -> `"key": value` for bare identifiers.
    .replace(/([{,\s])([A-Za-z_][A-Za-z0-9_-]*)\s*=/g, '$1"$2":')
    // `"key" = value` -> `"key": value`.
    .replace(/("(?:[^"\\]|\\.)*")\s*=/g, '$1:')
    // Newline-separated object members -> commas.
    .replace(/("|\d|true|false|null|\]|\})\s*\n\s*(")/g, '$1,\n$2')
    // Trailing commas.
    .replace(/,\s*([}\]])/g, '$1');
  try {
    return JSON.parse(asJson);
  } catch {
    return undefined;
  }
}

/** Next free "<base>-N" instance name. */
export function nextInstanceName(base: string, taken: string[]): string {
  const clean = base.toLowerCase().replace(/[^a-z0-9-]/g, '-').replace(/^-+|-+$/g, '').slice(0, 50) || 'instance';
  for (let i = 1; ; i++) {
    const candidate = `${clean}-${i}`;
    if (!taken.includes(candidate)) return candidate;
  }
}

/** Fields that need a full row rather than half the grid. */
export function isWideField(v: VarSchema): boolean {
  return ['map', 'object', 'tuple', 'any', 'unsupported'].includes(v.type) || isMultiline(v) || (isListOrSet(v) && !isStringList(v));
}

export function isListOrSet(v: VarSchema) {
  return v.type === 'list' || v.type === 'set';
}

export function isStringList(v: VarSchema) {
  return isListOrSet(v) && (v.element_type?.type === 'string' || /^(list|set)\(string\)$/.test(v.type_raw.replace(/\s/g, '')));
}

export function isStringMap(v: VarSchema) {
  return v.type === 'map' && (v.element_type?.type === 'string' || v.type_raw.replace(/\s/g, '') === 'map(string)');
}

export function isMultiline(v: VarSchema) {
  return v.type === 'string' && /script|policy|certificate|_pem$|_json$/.test(v.name);
}
