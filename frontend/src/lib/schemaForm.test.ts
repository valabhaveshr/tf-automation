import { describe, expect, it } from 'vitest';
import {
  enumOptions,
  groupVariables,
  humanize,
  nextInstanceName,
  parseExample,
  validateInstances,
  versionFromTag,
} from './schemaForm';
import type { ModuleSchema, VarSchema } from '../api/types';

/**
 * Sanity tests for the client-side rules called out in plan/ARCHITECTURE.md's
 * "Forms / UI" edge cases (at least one instance, required fields, instance
 * name charset) and the metadata.yaml-driven form layout. These are
 * convenience checks only — /api/records/preview is the authoritative
 * validator.
 */

function v(name: string, extra: Partial<VarSchema> = {}): VarSchema {
  return {
    name,
    type: 'string',
    type_raw: 'string',
    has_default: false,
    required: false,
    sensitive: false,
    nullable: true,
    validations: [],
    ...extra,
  };
}

const schema: ModuleSchema = {
  module_name: 'compute/instance',
  version_tag: 'modules/compute/instance/v1.2.0',
  metadata: {
    name: 'instance',
    display_name: 'Compute Instance',
    category: 'Compute',
    cloud: 'gcp',
    icon: 'server',
    description: 'Provisions a single VM.',
    instance_label_field: 'name',
    variables: [
      { name: 'name', example: 'web-01', group: 'Basics' },
      { name: 'image', example: 'debian-cloud/debian-12', group: 'Boot disk' },
      { name: 'size', example: 'e2-standard-2', group: 'Basics' },
      { name: 'secure_boot', example: 'true', group: 'Security', advanced: true },
      { name: 'zone', example: 'us-central1-a', group: 'Basics', advanced: true },
    ],
  },
  variables: [
    v('image', { required: true, nullable: false }),
    v('size', { has_default: true, default: 'e2-small' }),
    v('name', { required: true, nullable: false }),
    v('secure_boot', { type: 'bool', type_raw: 'bool', has_default: true, default: true }),
    v('zone', { required: true, nullable: false }),
    v('unhinted'),
  ],
};

describe('validateInstances', () => {
  it('requires at least one instance', () => {
    const errors = validateInstances(schema, {});
    expect(errors.some((e) => e.message.includes('at least one instance'))).toBe(true);
  });

  it('flags a required field left blank', () => {
    const errors = validateInstances(schema, { 'web-01': { image: '', name: 'a', zone: 'z' } });
    expect(errors.some((e) => e.instanceName === 'web-01' && e.varName === 'image')).toBe(true);
  });

  it('rejects an instance name outside the allowed charset', () => {
    const errors = validateInstances(schema, { Web_01: { image: 'i', name: 'a', zone: 'z' } });
    expect(errors.some((e) => e.instanceName === 'Web_01')).toBe(true);
  });

  it('passes when required fields are filled and names are valid', () => {
    const errors = validateInstances(schema, { 'web-01': { image: 'debian', name: 'web-01', zone: 'z' } });
    expect(errors).toHaveLength(0);
  });
});

describe('groupVariables', () => {
  it('orders by metadata hints, groups by section, and puts unhinted fields last', () => {
    const { sections, advanced } = groupVariables(schema);
    expect(sections.map((s) => [s.title, s.fields.map((f) => f.name)])).toEqual([
      ['Basics', ['name', 'size', 'zone']],
      ['Boot disk', ['image']],
      ['Other settings', ['unhinted']],
    ]);
    expect(advanced.map((s) => [s.title, s.fields.map((f) => f.name)])).toEqual([['Security', ['secure_boot']]]);
  });

  it('never hides a required field under Advanced', () => {
    const { advanced } = groupVariables(schema);
    expect(advanced.flatMap((s) => s.fields).some((f) => f.name === 'zone')).toBe(false);
  });
});

describe('enumOptions', () => {
  it('reads allowed values from a contains([...], var.x) validation', () => {
    const pm = v('provisioning_model', {
      validations: [{ condition_raw: 'contains(["STANDARD", "SPOT"], var.provisioning_model)', error_message: '' }],
    });
    expect(enumOptions(pm)).toEqual(['STANDARD', 'SPOT']);
  });

  it('ignores validations about other variables', () => {
    const other = v('mode', {
      validations: [{ condition_raw: 'contains(["A"], var.something_else)', error_message: '' }],
    });
    expect(enumOptions(other)).toBeNull();
  });
});

describe('parseExample', () => {
  it('maps examples onto field types', () => {
    expect(parseExample(v('s'), '"us-central1-a"')).toBe('us-central1-a');
    expect(parseExample(v('n', { type: 'number' }), '50')).toBe(50);
    expect(parseExample(v('b', { type: 'bool' }), 'false')).toBe(false);
    expect(parseExample(v('l', { type: 'list' }), '["web", "allow-ssh"]')).toEqual(['web', 'allow-ssh']);
    expect(parseExample(v('m', { type: 'map' }), '{ team = "platform", env = "prod" }')).toEqual({
      team: 'platform',
      env: 'prod',
    });
  });

  it('returns undefined when the example does not fit the type', () => {
    expect(parseExample(v('n', { type: 'number' }), 'lots')).toBeUndefined();
  });
});

describe('small helpers', () => {
  it('humanizes variable names', () => {
    expect(humanize('boot_disk_size_gb')).toBe('Boot disk size GB');
    expect(humanize('project_id')).toBe('Project ID');
  });

  it('extracts the version from a tag', () => {
    expect(versionFromTag('modules/compute/instance/v1.2.0')).toBe('v1.2.0');
  });

  it('picks the next free instance name', () => {
    expect(nextInstanceName('instance', ['instance-1'])).toBe('instance-2');
    expect(nextInstanceName('Project IAM', [])).toBe('project-iam-1');
  });
});
