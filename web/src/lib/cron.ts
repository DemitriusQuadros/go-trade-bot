import { tr, type MessageKey } from '@/i18n';

// describeCron turns the handful of cron shapes an operator actually uses
// for agent schedules into plain words ("every 6h", "daily at 00:00").
// Anything it doesn't recognise falls back to the raw spec - validation is
// the backend's job (A-02 rejects unparsable specs with a 400), this is
// display only. All agent schedules run in UTC.

const dow = (d: number) => tr(`cron.dow.d${d}` as MessageKey);

const pad = (n: number) => String(n).padStart(2, '0');
const isNum = (s: string) => /^\d+$/.test(s);

export function describeCron(spec: string): string {
  const raw = spec.trim();
  if (!raw) return '';

  const descriptors: Record<string, string> = {
    '@hourly': tr('cron.hourly'),
    '@daily': tr('cron.daily0'),
    '@midnight': tr('cron.daily0'),
    '@weekly': tr('cron.weekly0'),
    '@monthly': tr('cron.monthly0'),
    '@yearly': tr('cron.yearly0'),
    '@annually': tr('cron.yearly0'),
  };
  if (descriptors[raw]) return descriptors[raw];
  const every = raw.match(/^@every\s+(\S+)$/);
  if (every) return tr('cron.every', { value: every[1] });

  const parts = raw.split(/\s+/);
  if (parts.length !== 5) return raw;
  const [min, hour, dom, mon, dowSpec] = parts;
  const anyDate = dom === '*' && mon === '*';

  if (min === '*' && hour === '*' && anyDate && dowSpec === '*') return tr('cron.everyMinute');

  const minStep = min.match(/^\*\/(\d+)$/);
  if (minStep && hour === '*' && anyDate && dowSpec === '*') return tr('cron.every', { value: `${minStep[1]}m` });

  if (isNum(min) && hour === '*' && anyDate && dowSpec === '*') {
    return min === '0' ? tr('cron.hourly') : tr('cron.hourlyAt', { min: pad(Number(min)) });
  }

  const hourStep = hour.match(/^\*\/(\d+)$/);
  if (isNum(min) && hourStep && anyDate && dowSpec === '*') {
    return min === '0'
      ? tr('cron.everyHours', { hours: hourStep[1] })
      : tr('cron.everyHoursAt', { hours: hourStep[1], min: pad(Number(min)) });
  }

  if (isNum(min) && isNum(hour) && anyDate) {
    const at = `${pad(Number(hour))}:${pad(Number(min))}`;
    if (dowSpec === '*') return tr('cron.dailyAt', { at });
    if (dowSpec === '1-5') return tr('cron.weekdaysAt', { at });
    if (isNum(dowSpec) && Number(dowSpec) <= 7) return tr('cron.weeklyAt', { day: dow(Number(dowSpec) % 7), at });
  }

  return raw;
}

export const CRON_PRESETS: { readonly label: string; spec: string }[] = [
  { spec: '*/15 * * * *', key: 'cron.presetEvery15m' },
  { spec: '*/30 * * * *', key: 'cron.presetEvery30m' },
  { spec: '0 * * * *', key: 'cron.presetHourly' },
  { spec: '0 */6 * * *', key: 'cron.presetEvery6h' },
  { spec: '0 0 * * *', key: 'cron.presetDaily' },
  { spec: '0 9 * * 1-5', key: 'cron.presetWeekdays9am' },
].map(({ spec, key }) => ({
  spec,
  get label() {
    return tr(key as MessageKey);
  },
}));

