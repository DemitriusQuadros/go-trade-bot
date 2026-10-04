import React, { useState, useMemo } from 'react';
import { Clock, Plus, Sparkles, SlidersHorizontal, Calendar, CalendarDays, Repeat } from 'lucide-react';
import { CRON_PRESETS, describeCron } from '@/lib/cron';
import { useT, type MessageKey } from '@/i18n';

type BuilderMode = 'interval' | 'daily' | 'weekdays' | 'weekly' | 'advanced';
type IntervalUnit = 'minutes' | 'hours';

interface CronScheduleBuilderProps {
  existingSchedules: string[];
  onAddSchedule: (cronSpec: string) => void;
}

const MINUTE_STEPS = [5, 10, 15, 20, 30, 45];
const HOUR_STEPS = [1, 2, 3, 4, 6, 8, 12];
const MINUTE_OPTIONS = [0, 5, 10, 15, 20, 25, 30, 35, 40, 45, 50, 55];
const HOUR_OPTIONS = Array.from({ length: 24 }, (_, i) => i);
const DAYS_OF_WEEK = [0, 1, 2, 3, 4, 5, 6];

const pad = (n: number) => String(n).padStart(2, '0');

export function CronScheduleBuilder({ existingSchedules, onAddSchedule }: CronScheduleBuilderProps) {
  const t = useT();

  const [mode, setMode] = useState<BuilderMode>('interval');

  // Interval state
  const [intervalUnit, setIntervalUnit] = useState<IntervalUnit>('minutes');
  const [intervalMinutes, setIntervalMinutes] = useState<number>(15);
  const [intervalHours, setIntervalHours] = useState<number>(6);
  const [intervalAtMinute, setIntervalAtMinute] = useState<number>(0);

  // Time state (for daily, weekdays, weekly)
  const [timeHour, setTimeHour] = useState<number>(9);
  const [timeMinute, setTimeMinute] = useState<number>(0);

  // Weekly state
  const [weeklyDay, setWeeklyDay] = useState<number>(1); // Monday

  // Advanced raw state
  const [customCron, setCustomCron] = useState<string>('');

  // Compute the current cron expression based on selected mode and state
  const compiledCron = useMemo(() => {
    switch (mode) {
      case 'interval':
        if (intervalUnit === 'minutes') {
          return `*/${intervalMinutes} * * * *`;
        }
        return `${intervalAtMinute} */${intervalHours} * * *`;
      case 'daily':
        return `${timeMinute} ${timeHour} * * *`;
      case 'weekdays':
        return `${timeMinute} ${timeHour} * * 1-5`;
      case 'weekly':
        return `${timeMinute} ${timeHour} * * ${weeklyDay}`;
      case 'advanced':
        return customCron.trim();
    }
  }, [mode, intervalUnit, intervalMinutes, intervalHours, intervalAtMinute, timeHour, timeMinute, weeklyDay, customCron]);

  const isAlreadyAdded = compiledCron ? existingSchedules.includes(compiledCron) : false;
  const isValid = Boolean(compiledCron && (mode !== 'advanced' || compiledCron.split(/\s+/).length === 5));

  const handleAdd = () => {
    if (!isValid || isAlreadyAdded) return;
    onAddSchedule(compiledCron);
    if (mode === 'advanced') {
      setCustomCron('');
    }
  };

  return (
    <div className="rounded-lg border border-border bg-card/60 p-3.5 space-y-3.5">
      <div className="flex items-center justify-between gap-2 border-b border-border/60 pb-2.5">
        <div className="flex items-center gap-1.5 text-xs font-semibold text-foreground">
          <Sparkles className="w-3.5 h-3.5 text-primary" />
          <span>{t('cron.builderTitle')}</span>
        </div>
        <span className="hidden sm:inline text-[11px] text-muted-foreground">{t('cron.builderHelp')}</span>
      </div>

      {/* Mode navigation tabs */}
      <div className="flex flex-wrap items-center gap-1">
        <button
          type="button"
          onClick={() => setMode('interval')}
          className={`flex items-center gap-1 px-2.5 py-1 rounded text-xs font-medium transition-colors ${
            mode === 'interval' ? 'bg-primary text-primary-foreground' : 'bg-secondary text-muted-foreground hover:text-foreground'
          }`}
        >
          <Repeat className="w-3 h-3" />
          {t('cron.modeInterval')}
        </button>

        <button
          type="button"
          onClick={() => setMode('daily')}
          className={`flex items-center gap-1 px-2.5 py-1 rounded text-xs font-medium transition-colors ${
            mode === 'daily' ? 'bg-primary text-primary-foreground' : 'bg-secondary text-muted-foreground hover:text-foreground'
          }`}
        >
          <Clock className="w-3 h-3" />
          {t('cron.modeDaily')}
        </button>

        <button
          type="button"
          onClick={() => setMode('weekdays')}
          className={`flex items-center gap-1 px-2.5 py-1 rounded text-xs font-medium transition-colors ${
            mode === 'weekdays' ? 'bg-primary text-primary-foreground' : 'bg-secondary text-muted-foreground hover:text-foreground'
          }`}
        >
          <CalendarDays className="w-3 h-3" />
          {t('cron.modeWeekdays')}
        </button>

        <button
          type="button"
          onClick={() => setMode('weekly')}
          className={`flex items-center gap-1 px-2.5 py-1 rounded text-xs font-medium transition-colors ${
            mode === 'weekly' ? 'bg-primary text-primary-foreground' : 'bg-secondary text-muted-foreground hover:text-foreground'
          }`}
        >
          <Calendar className="w-3 h-3" />
          {t('cron.modeWeekly')}
        </button>

        <button
          type="button"
          onClick={() => setMode('advanced')}
          className={`flex items-center gap-1 px-2.5 py-1 rounded text-xs font-medium transition-colors ${
            mode === 'advanced' ? 'bg-primary text-primary-foreground' : 'bg-secondary text-muted-foreground hover:text-foreground'
          }`}
        >
          <SlidersHorizontal className="w-3 h-3" />
          {t('cron.modeAdvanced')}
        </button>
      </div>

      {/* Mode-specific visual controls */}
      <div className="bg-background/80 rounded-md border border-border p-3 space-y-3">
        {mode === 'interval' && (
          <div className="flex flex-wrap items-center gap-3">
            <div className="flex items-center gap-1.5">
              <label htmlFor="interval-unit-select" className="text-xs text-muted-foreground font-medium">
                {t('cron.unitMinutes')} / {t('cron.unitHours')}:
              </label>
              <select
                id="interval-unit-select"
                value={intervalUnit}
                onChange={(e) => setIntervalUnit(e.target.value as IntervalUnit)}
                className="form-select text-xs py-1 px-2.5 bg-card border-border rounded"
              >
                <option value="minutes">{t('cron.unitMinutes')}</option>
                <option value="hours">{t('cron.unitHours')}</option>
              </select>
            </div>

            {intervalUnit === 'minutes' ? (
              <div className="flex items-center gap-1.5">
                <label htmlFor="interval-minutes-select" className="text-xs text-muted-foreground font-medium">
                  {t('cron.everyMinutes', { count: '' }).replace('{count}', '').trim()}:
                </label>
                <select
                  id="interval-minutes-select"
                  value={intervalMinutes}
                  onChange={(e) => setIntervalMinutes(Number(e.target.value))}
                  className="form-select text-xs py-1 px-2.5 bg-card border-border rounded"
                >
                  {MINUTE_STEPS.map((m) => (
                    <option key={m} value={m}>
                      {m}
                    </option>
                  ))}
                </select>
              </div>
            ) : (
              <div className="flex flex-wrap items-center gap-3">
                <div className="flex items-center gap-1.5">
                  <label htmlFor="interval-hours-select" className="text-xs text-muted-foreground font-medium">
                    {t('cron.everyHoursUnit', { count: '' }).replace('{count}', '').trim()}:
                  </label>
                  <select
                    id="interval-hours-select"
                    value={intervalHours}
                    onChange={(e) => setIntervalHours(Number(e.target.value))}
                    className="form-select text-xs py-1 px-2.5 bg-card border-border rounded"
                  >
                    {HOUR_STEPS.map((h) => (
                      <option key={h} value={h}>
                        {h}
                      </option>
                    ))}
                  </select>
                </div>
                <div className="flex items-center gap-1.5">
                  <label htmlFor="interval-minute-at-select" className="text-xs text-muted-foreground font-medium">
                    {t('cron.minuteLabel')}:
                  </label>
                  <select
                    id="interval-minute-at-select"
                    value={intervalAtMinute}
                    onChange={(e) => setIntervalAtMinute(Number(e.target.value))}
                    className="form-select text-xs py-1 px-2.5 bg-card border-border rounded"
                  >
                    {[0, 15, 30, 45].map((min) => (
                      <option key={min} value={min}>
                        :{pad(min)}
                      </option>
                    ))}
                  </select>
                </div>
              </div>
            )}
          </div>
        )}

        {mode === 'daily' && (
          <div className="flex items-center gap-2">
            <span className="text-xs text-muted-foreground font-medium">{t('cron.timeLabel')}:</span>
            <div className="flex items-center gap-1">
              <select
                value={timeHour}
                aria-label={t('cron.hourLabel')}
                onChange={(e) => setTimeHour(Number(e.target.value))}
                className="form-select text-xs py-1 px-2 bg-card border-border rounded"
              >
                {HOUR_OPTIONS.map((h) => (
                  <option key={h} value={h}>
                    {pad(h)}
                  </option>
                ))}
              </select>
              <span className="text-muted-foreground">:</span>
              <select
                value={timeMinute}
                aria-label={t('cron.minuteLabel')}
                onChange={(e) => setTimeMinute(Number(e.target.value))}
                className="form-select text-xs py-1 px-2 bg-card border-border rounded"
              >
                {MINUTE_OPTIONS.map((m) => (
                  <option key={m} value={m}>
                    {pad(m)}
                  </option>
                ))}
              </select>
              <span className="text-xs text-muted-foreground ml-1">UTC</span>
            </div>
          </div>
        )}

        {mode === 'weekdays' && (
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-xs text-muted-foreground font-medium">{t('cron.timeLabel')}:</span>
            <div className="flex items-center gap-1">
              <select
                value={timeHour}
                aria-label={t('cron.hourLabel')}
                onChange={(e) => setTimeHour(Number(e.target.value))}
                className="form-select text-xs py-1 px-2 bg-card border-border rounded"
              >
                {HOUR_OPTIONS.map((h) => (
                  <option key={h} value={h}>
                    {pad(h)}
                  </option>
                ))}
              </select>
              <span className="text-muted-foreground">:</span>
              <select
                value={timeMinute}
                aria-label={t('cron.minuteLabel')}
                onChange={(e) => setTimeMinute(Number(e.target.value))}
                className="form-select text-xs py-1 px-2 bg-card border-border rounded"
              >
                {MINUTE_OPTIONS.map((m) => (
                  <option key={m} value={m}>
                    {pad(m)}
                  </option>
                ))}
              </select>
              <span className="text-xs text-muted-foreground ml-1">UTC (Mon-Fri)</span>
            </div>
          </div>
        )}

        {mode === 'weekly' && (
          <div className="flex flex-wrap items-center gap-3">
            <div className="flex items-center gap-1.5">
              <label htmlFor="weekly-day-select" className="text-xs text-muted-foreground font-medium">
                {t('cron.dayLabel')}:
              </label>
              <select
                id="weekly-day-select"
                value={weeklyDay}
                onChange={(e) => setWeeklyDay(Number(e.target.value))}
                className="form-select text-xs py-1 px-2.5 bg-card border-border rounded"
              >
                {DAYS_OF_WEEK.map((d) => (
                  <option key={d} value={d}>
                    {t(`cron.dowFull.d${d}` as MessageKey)}
                  </option>
                ))}
              </select>
            </div>
            <div className="flex items-center gap-1">
              <select
                value={timeHour}
                aria-label={t('cron.hourLabel')}
                onChange={(e) => setTimeHour(Number(e.target.value))}
                className="form-select text-xs py-1 px-2 bg-card border-border rounded"
              >
                {HOUR_OPTIONS.map((h) => (
                  <option key={h} value={h}>
                    {pad(h)}
                  </option>
                ))}
              </select>
              <span className="text-muted-foreground">:</span>
              <select
                value={timeMinute}
                aria-label={t('cron.minuteLabel')}
                onChange={(e) => setTimeMinute(Number(e.target.value))}
                className="form-select text-xs py-1 px-2 bg-card border-border rounded"
              >
                {MINUTE_OPTIONS.map((m) => (
                  <option key={m} value={m}>
                    {pad(m)}
                  </option>
                ))}
              </select>
              <span className="text-xs text-muted-foreground ml-1">UTC</span>
            </div>
          </div>
        )}

        {mode === 'advanced' && (
          <div className="flex items-center gap-2">
            <input
              type="text"
              value={customCron}
              onChange={(e) => setCustomCron(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') {
                  e.preventDefault();
                  handleAdd();
                }
              }}
              placeholder={t('cron.customPlaceholder')}
              aria-label={t('agentEditor.customCronAria')}
              className="form-input text-xs font-mono py-1.5 flex-1"
            />
          </div>
        )}
      </div>

      {/* Quick presets chips */}
      <div className="flex flex-wrap items-center gap-1.5 pt-1">
        <span className="text-[11px] text-muted-foreground font-medium">{t('agentEditor.addLabel')}</span>
        {CRON_PRESETS.map((p) => {
          const added = existingSchedules.includes(p.spec);
          return (
            <button
              key={p.spec}
              type="button"
              onClick={() => onAddSchedule(p.spec)}
              disabled={added}
              className="bg-secondary hover:bg-accent text-foreground rounded border border-border text-[11px] px-2 py-0.5 disabled:opacity-40 transition-colors"
              title={p.spec}
            >
              {p.label}
            </button>
          );
        })}
      </div>

      {/* Preview & Action bar */}
      <div className="flex flex-wrap items-center justify-between gap-2 pt-2 border-t border-border/60">
        <div className="flex items-center gap-2 min-w-0">
          <span className="font-mono text-xs px-2 py-0.5 rounded bg-muted text-foreground border border-border shrink-0">
            {compiledCron || '—'}
          </span>
          <span className="text-xs text-foreground font-medium truncate">
            {describeCron(compiledCron)} <span className="text-muted-foreground">UTC</span>
          </span>
        </div>

        <button
          type="button"
          onClick={handleAdd}
          disabled={!isValid || isAlreadyAdded}
          className="bg-primary hover:bg-primary/90 text-primary-foreground text-xs font-medium px-3 py-1.5 rounded flex items-center gap-1 disabled:opacity-40 transition-colors shrink-0"
        >
          <Plus className="w-3.5 h-3.5" />
          {isAlreadyAdded ? t('cron.builderAlreadyAdded') : t('cron.builderAdd')}
        </button>
      </div>
    </div>
  );
}
