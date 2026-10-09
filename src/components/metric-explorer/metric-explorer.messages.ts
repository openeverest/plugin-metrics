import { MetricType } from 'types/metrics.types';

const TYPE_LABELS: Record<MetricType, string> = {
  counter: 'Counter · per-second rate',
  gauge: 'Gauge',
  histogram: 'Histogram · p99',
  summary: 'Summary · average',
  unknown: 'Untyped',
};

export const Messages = {
  placeholder: 'Add a metric — type to search by name or description',
  loading: 'Loading metrics…',
  noOptions: 'No matching metrics',
  loadFailed: (reason: string) => `Failed to list metrics: ${reason}`,
  remove: 'Remove metric',
  otherGroup: 'Other',
  typeLabel: (type: MetricType) => TYPE_LABELS[type],
  caption: (type: MetricType, help?: string) =>
    help ? `${TYPE_LABELS[type]} — ${help}` : TYPE_LABELS[type],
};
