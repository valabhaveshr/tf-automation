import { describe, expect, it } from 'vitest';
import { validateInstances } from './ModuleInstanceForm';
import type { ModuleSchema } from '../api/types';

/**
 * Sanity tests for the client-side validation rules called out in
 * plan/ARCHITECTURE.md's "Forms / UI" edge cases: at least one instance
 * required, required-field-blank blocked, and instance-name charset
 * enforced. These are convenience checks only — /api/records/preview is
 * the authoritative validator (see ModuleInstanceForm.tsx's file comment).
 */

const schema: ModuleSchema = {
  module_name: 'vm-instance',
  version_tag: 'vm-instance/v1.2.0',
  metadata: {
    name: 'vm-instance',
    display_name: 'Virtual Machine Instance',
    category: 'Compute',
    cloud: 'azure',
    icon: 'server',
    description: 'Provisions a single VM.',
    instance_label_field: 'name',
    naming_resource_type: null,
    naming_target_variable: null,
  },
  variables: [
    {
      name: 'image',
      type: 'string',
      type_raw: 'string',
      has_default: false,
      required: true,
      sensitive: false,
      nullable: false,
      validations: [],
    },
    {
      name: 'size',
      type: 'string',
      type_raw: 'string',
      default: 'Standard_B2s',
      has_default: true,
      required: false,
      sensitive: false,
      nullable: false,
      validations: [],
    },
  ],
};

describe('validateInstances', () => {
  it('requires at least one instance', () => {
    const errors = validateInstances(schema, {});
    expect(errors.some((e) => e.message.includes('at least one instance'))).toBe(true);
  });

  it('flags a required field left blank', () => {
    const errors = validateInstances(schema, { 'web-01': { image: '' } });
    expect(errors.some((e) => e.instanceName === 'web-01' && e.varName === 'image')).toBe(true);
  });

  it('rejects an instance name outside the allowed charset', () => {
    const errors = validateInstances(schema, { Web_01: { image: 'ubuntu' } });
    expect(errors.some((e) => e.instanceName === 'Web_01')).toBe(true);
  });

  it('passes when required fields are filled and names are valid', () => {
    const errors = validateInstances(schema, { 'web-01': { image: 'ubuntu-22.04' } });
    expect(errors).toHaveLength(0);
  });
});
