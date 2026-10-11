const rtf = new Intl.RelativeTimeFormat(undefined, { numeric: 'auto' });

/** "3 minutes ago", "yesterday", ... */
export function relativeTime(iso: string | null | undefined): string {
  if (!iso) return '';
  const then = new Date(iso).getTime();
  if (Number.isNaN(then)) return '';
  const diffSec = Math.round((then - Date.now()) / 1000);
  const units: [Intl.RelativeTimeFormatUnit, number][] = [
    ['year', 31536000],
    ['month', 2592000],
    ['week', 604800],
    ['day', 86400],
    ['hour', 3600],
    ['minute', 60],
  ];
  for (const [unit, secs] of units) {
    if (Math.abs(diffSec) >= secs) return rtf.format(Math.round(diffSec / secs), unit);
  }
  return 'just now';
}

export function formatDateTime(iso: string | null | undefined): string {
  if (!iso) return '';
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? '' : d.toLocaleString();
}

/** Short names are stored lowercase but shown uppercase, e.g. "DMSC". */
export function shortCode(code: string | null | undefined): string {
  return (code ?? '').toUpperCase();
}

/** Shows a value compactly for summaries: strings as-is, everything else as JSON. */
export function displayValue(v: unknown): string {
  if (v === undefined) return '';
  if (v === null) return 'null';
  if (typeof v === 'string') return v;
  return JSON.stringify(v);
}

export function errorMessage(err: unknown, fallback: string): string {
  return err instanceof Error && err.message ? err.message : fallback;
}

/** Mirrors backend/internal/api/slug.go, so the UI shows the exact output path a save will use. */
export function slugify(s: string): string {
  const out = s.trim().toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '');
  return out || 'x';
}

export function outputPath(customer: string, project: string, env: string): string {
  if (!customer || !project || !env) return '';
  return `${slugify(customer)}/${slugify(project)}/${slugify(env)}`;
}

/** Badge color for an environment name: prod red, staging orange, test yellow, dev teal. */
export function envColor(env: string): string {
  const e = env.toLowerCase();
  if (/prod/.test(e)) return 'red';
  if (/stag|uat|pre/.test(e)) return 'orange';
  if (/test|qa/.test(e)) return 'yellow';
  if (/dev|sandbox/.test(e)) return 'teal';
  return 'gray';
}
