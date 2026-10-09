import { SourceReason, TimeRange } from 'types/metrics.types';

const SOURCE_NAMES: Record<string, string> = {
  prometheus: 'Prometheus',
};

const sourceName = (type: string) => SOURCE_NAMES[type] ?? type;

export const Messages = {
  tabLabel: 'Metrics',
  loadFailed: (reason: string) => `Failed to load metrics: ${reason}`,
  timeRanges: {
    '1h': 'Last hour',
    '24h': 'Last 24 hours',
  } satisfies Record<TimeRange, string>,
  source: (type: string) => `Source: ${sourceName(type)}`,
  sourceUnavailable: {
    notEnabled: (type: string) =>
      `${sourceName(type)} monitoring is not enabled for this instance. Enable it in the instance's Monitoring settings.`,
    unavailable: (type: string) => `${sourceName(type)} is not available in this cluster.`,
  } satisfies Record<SourceReason, (type: string) => string>,
  noDashboard: 'There is no metrics dashboard for this database engine yet.',
};
