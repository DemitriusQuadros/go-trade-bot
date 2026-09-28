// describeCron turns the handful of cron shapes an operator actually uses
// for agent schedules into plain words ("every 6h", "daily at 00:00").
// Anything it doesn't recognise falls back to the raw spec - validation is
// the backend's job (A-02 rejects unparsable specs with a 400), this is
// display only. All agent schedules run in UTC.

const DOW = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];

const pad = (n: number) => String(n).padStart(2, '0');
const isNum = (s: string) => /^\d+$/.test(s);

export function describeCron(spec: string): string {
  const raw = spec.trim();
  if (!raw) return '';

  const descriptors: Record<string, string> = {
    '@hourly': 'hourly',
    '@daily': 'daily at 00:00',
    '@midnight': 'daily at 00:00',
    '@weekly': 'weekly (Sun 00:00)',
    '@monthly': 'monthly (1st, 00:00)',
    '@yearly': 'yearly (Jan 1, 00:00)',
    '@annually': 'yearly (Jan 1, 00:00)',
  };
  if (descriptors[raw]) return descriptors[raw];
  const every = raw.match(/^@every\s+(\S+)$/);
  if (every) return `every ${every[1]}`;

  const parts = raw.split(/\s+/);
  if (parts.length !== 5) return raw;
  const [min, hour, dom, mon, dow] = parts;
  const anyDate = dom === '*' && mon === '*';

  if (min === '*' && hour === '*' && anyDate && dow === '*') return 'every minute';

  const minStep = min.match(/^\*\/(\d+)$/);
  if (minStep && hour === '*' && anyDate && dow === '*') return `every ${minStep[1]}m`;

  if (isNum(min) && hour === '*' && anyDate && dow === '*') {
    return min === '0' ? 'hourly' : `hourly at :${pad(Number(min))}`;
  }

  const hourStep = hour.match(/^\*\/(\d+)$/);
  if (isNum(min) && hourStep && anyDate && dow === '*') {
    return `every ${hourStep[1]}h${min === '0' ? '' : ` at :${pad(Number(min))}`}`;
  }

  if (isNum(min) && isNum(hour) && anyDate) {
    const at = `${pad(Number(hour))}:${pad(Number(min))}`;
    if (dow === '*') return `daily at ${at}`;
    if (dow === '1-5') return `weekdays at ${at}`;
    if (isNum(dow) && Number(dow) <= 7) return `weekly ${DOW[Number(dow) % 7]} ${at}`;
  }

  return raw;
}

export const CRON_PRESETS: { label: string; spec: string }[] = [
  { label: 'Hourly', spec: '0 * * * *' },
  { label: 'Every 6h', spec: '0 */6 * * *' },
  { label: 'Daily 00:00 UTC', spec: '0 0 * * *' },
];
