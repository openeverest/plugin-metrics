import { MetricSeries, MetricUnit } from 'types/metrics.types';
import { BYTE_UNITS } from './metric-panel.constants';
import { Messages } from './metric-panel.messages';

export interface ChartSeries {
  label: string;
  data: (number | null)[];
}

export interface ChartData {
  timestamps: Date[];
  series: ChartSeries[];
}

// The chart needs one shared x axis; a series is null where it has no sample.
export const toChartData = (series: MetricSeries[], fallbackLabel: string): ChartData => {
  const times = [...new Set(series.flatMap((s) => s.points.map((p) => p.t)))].sort(
    (a, b) => a - b
  );
  return {
    timestamps: times.map((t) => new Date(t)),
    series: series.map((s) => {
      const valueAt = new Map(s.points.map((p) => [p.t, p.v]));
      return { label: s.name || fallbackLabel, data: times.map((t) => valueAt.get(t) ?? null) };
    }),
  };
};

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
