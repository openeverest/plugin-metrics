import { MetricSeries, MetricUnit } from 'types/metrics.types';
import { BYTE_UNITS, SERIES_COLORS, Y_TICK_COUNT } from './metric-panel.constants';
import { Messages } from './metric-panel.messages';

export interface ChartSeries {
  label: string;
  color: string;
  data: (number | null)[];
  /** The most recent non-null sample, shown in the legend. */
  latest: number | null;
}

export interface ChartData {
  timestamps: Date[];
  series: ChartSeries[];
}

const latestValue = (data: (number | null)[]) => {
  for (let i = data.length - 1; i >= 0; i--) {
    const value = data[i];
    if (value !== null) {
      return value;
    }
  }
  return null;
};

// The chart needs one shared x axis; a series is null where it has no sample.
export const toChartData = (series: MetricSeries[], fallbackLabel: string): ChartData => {
  const times = [...new Set(series.flatMap((s) => s.points.map((p) => p.t)))].sort(
    (a, b) => a - b
  );
  return {
    timestamps: times.map((t) => new Date(t)),
    series: series.map((s, i) => {
      const valueAt = new Map(s.points.map((p) => [p.t, p.v]));
      const data = times.map((t) => valueAt.get(t) ?? null);
      return {
        label: s.name || fallbackLabel,
        color: SERIES_COLORS[i % SERIES_COLORS.length],
        data,
        latest: latestValue(data),
      };
    }),
  };
};

// Rounds a step up to 1, 2, 2.5 or 5 times a power of ten.
const niceStep = (rough: number) => {
  const magnitude = 10 ** Math.floor(Math.log10(rough));
  const normalized = rough / magnitude;
  const nice = [1, 2, 2.5, 5, 10].find((n) => normalized <= n) ?? 10;
  return nice * magnitude;
};

// Byte axes need steps that are round in binary units (e.g. 200 MiB); decimal
// nice ticks render as 286.1 MiB.
export const byteTicks = (max: number): number[] | undefined => {
  if (max <= 0) {
    return undefined;
  }
  const exponent = Math.min(Math.floor(Math.log(max) / Math.log(1024)), BYTE_UNITS.length - 1);
  const unitSize = 1024 ** exponent;
  const step = niceStep(max / unitSize / Y_TICK_COUNT) * unitSize;
  const ticks = [0];
  while (ticks[ticks.length - 1] < max) {
    ticks.push(ticks[ticks.length - 1] + step);
  }
  return ticks;
};

export const seriesMax = (series: ChartSeries[]) =>
  Math.max(0, ...series.flatMap((s) => s.data.filter((v): v is number => v !== null)));

const formatNumber = (value: number) =>
  value.toLocaleString(
    undefined,
    Math.abs(value) < 1 ? { maximumSignificantDigits: 2 } : { maximumFractionDigits: 2 }
  );

const formatBytes = (value: number) => {
  let scaled = value;
  let unit = 0;
  while (Math.abs(scaled) >= 1024 && unit < BYTE_UNITS.length - 1) {
    scaled /= 1024;
    unit++;
  }
  return `${formatNumber(scaled)} ${BYTE_UNITS[unit]}`;
};

export const formatValue = (value: number, unit: MetricUnit): string => {
  switch (unit) {
    case 'bytes':
      return formatBytes(value);
    case 'ms':
      return Math.abs(value) >= 1000
        ? Messages.seconds(formatNumber(value / 1000))
        : Messages.milliseconds(formatNumber(value));
    case 'ops':
      return Messages.perSecond(formatNumber(value));
    case 'cores':
      return Messages.cores(formatNumber(value));
    default:
      return formatNumber(value);
  }
};

export const formatTime = (date: Date) =>
  date.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' });
