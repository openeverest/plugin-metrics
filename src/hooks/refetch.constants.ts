import { TimeRange } from 'types/metrics.types';

// Matches the backend step of each range, so every refresh adds a point.
export const RANGE_REFETCH_INTERVAL_MS: Record<TimeRange, number> = {
  '1h': 30_000,
  '24h': 300_000,
};
