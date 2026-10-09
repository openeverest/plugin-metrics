export const CHART_HEIGHT = 220;

export const BYTE_UNITS = ['B', 'KiB', 'MiB', 'GiB', 'TiB'];

// Distinct on both light and dark backgrounds; series keep their color across ranges
// because the backend returns them sorted by name.
export const SERIES_COLORS = [
  '#6366F1',
  '#14B8A6',
  '#F59E0B',
  '#EC4899',
  '#0EA5E9',
  '#8B5CF6',
  '#22C55E',
  '#EF4444',
];

export const Y_TICK_COUNT = 4;

// Overlapping fills stack up, so busy panels get fainter ones.
export const AREA_OPACITY = { few: 0.28, many: 0.08 };
export const MAX_SERIES_FOR_STRONG_FILL = 2;
