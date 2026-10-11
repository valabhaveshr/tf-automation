import type { Customer, CustomerProfileInput } from '../api/types';

export const EMPTY_PROFILE: CustomerProfileInput = {
  name: '',
  business_short_code: '',
  contact_name: '',
  contact_email: '',
  contact_phone: '',
  country: '',
  industry: '',
  default_cloud: 'gcp',
  default_region: '',
  gcp_organization_id: '',
  billing_account_id: '',
};

export const SHORT_CODE = /^[A-Za-z0-9]{1,5}$/;
const STOP_WORDS = new Set(['of', 'and', 'the', 'for', 'a', 'an', '&']);

/** "Doha Marine Sports Centre" -> "DMSC" */
export function suggestShortCode(orgName: string): string {
  return orgName
    .split(/[^A-Za-z0-9&]+/)
    .filter((w) => w && !STOP_WORDS.has(w.toLowerCase()))
    .map((w) => w[0])
    .join('')
    .toUpperCase()
    .slice(0, 5);
}

/** The editable part of a customer record. */
export function toProfile(c: Customer): CustomerProfileInput {
  return {
    name: c.name,
    business_short_code: c.business_short_code,
    contact_name: c.contact_name,
    contact_email: c.contact_email,
    contact_phone: c.contact_phone,
    country: c.country,
    industry: c.industry,
    default_cloud: c.default_cloud,
    default_region: c.default_region,
    gcp_organization_id: c.gcp_organization_id,
    billing_account_id: c.billing_account_id,
  };
}
