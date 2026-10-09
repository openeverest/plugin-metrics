import { TimeRange } from 'types/metrics.types';

export const TIME_RANGES: TimeRange[] = ['1h', '24h'];

export const DEFAULT_TIME_RANGE: TimeRange = '1h';

export const PANEL_GRID_SX = {
  display: 'grid',
  gap: 2,
  gridTemplateColumns: { xs: '1fr', lg: '1fr 1fr' },
};
