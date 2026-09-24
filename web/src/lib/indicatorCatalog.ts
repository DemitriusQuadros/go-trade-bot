import { IndicatorKey, Comparator } from '@/api/types';

export interface IndicatorOption {
  key: IndicatorKey;
  label: string;
  params: { key: string; label: string; default: number }[];
}

export const INDICATOR_OPTIONS: IndicatorOption[] = [
  { key: 'rsi', label: 'RSI', params: [{ key: 'period', label: 'Period', default: 14 }] },
  { key: 'ema', label: 'EMA', params: [{ key: 'period', label: 'Period', default: 20 }] },
  { key: 'sma', label: 'SMA', params: [{ key: 'period', label: 'Period', default: 20 }] },
  { key: 'atr', label: 'ATR', params: [{ key: 'period', label: 'Period', default: 14 }] },
  {
    key: 'bollinger_upper',
    label: 'Bollinger Upper',
    params: [
      { key: 'period', label: 'Period', default: 20 },
      { key: 'std_dev', label: 'Std Dev', default: 2.0 },
    ],
  },
  {
    key: 'bollinger_middle',
    label: 'Bollinger Middle',
    params: [
      { key: 'period', label: 'Period', default: 20 },
      { key: 'std_dev', label: 'Std Dev', default: 2.0 },
    ],
  },
  {
    key: 'bollinger_lower',
    label: 'Bollinger Lower',
    params: [
      { key: 'period', label: 'Period', default: 20 },
      { key: 'std_dev', label: 'Std Dev', default: 2.0 },
    ],
  },
  {
    key: 'macd',
    label: 'MACD Line',
    params: [
      { key: 'fast', label: 'Fast', default: 12 },
      { key: 'slow', label: 'Slow', default: 26 },
      { key: 'signal', label: 'Signal', default: 9 },
    ],
  },
  {
    key: 'macd_signal',
    label: 'MACD Signal',
    params: [
      { key: 'fast', label: 'Fast', default: 12 },
      { key: 'slow', label: 'Slow', default: 26 },
      { key: 'signal', label: 'Signal', default: 9 },
    ],
  },
  {
    key: 'macd_histogram',
    label: 'MACD Histogram',
    params: [
      { key: 'fast', label: 'Fast', default: 12 },
      { key: 'slow', label: 'Slow', default: 26 },
      { key: 'signal', label: 'Signal', default: 9 },
    ],
  },
  { key: 'price', label: 'Price', params: [] },
];

// Note: Volume Filter and Price Change % were part of the original condition
// picker spec but are not implemented on the backend (app/strategies/template
// only supports indicator_threshold, indicator_crossing, price_level). They
// are intentionally excluded here so the builder never offers them.
export const NON_INDICATOR_FIELDS: { field: 'price'; label: string }[] = [
  { field: 'price', label: 'Price' },
];

export const COMPARATORS: { op: Comparator; label: string; isCrossing: boolean }[] = [
  { op: '>', label: '>', isCrossing: false },
  { op: '<', label: '<', isCrossing: false },
  { op: '>=', label: '>=', isCrossing: false },
  { op: '<=', label: '<=', isCrossing: false },
  { op: 'crosses_above', label: 'crosses above', isCrossing: true },
  { op: 'crosses_below', label: 'crosses below', isCrossing: true },
];
