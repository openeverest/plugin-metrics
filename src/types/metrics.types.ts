export type TimeRange = '1h' | '24h';

export type MetricUnit = '' | 'cores' | 'bytes' | 'Bps' | 'ops' | 'ms';

export type SourceReason = 'notEnabled' | 'unavailable';

export interface MetricsSource {
  type: string;
  enabled: boolean;
  /** The source can list and chart the instance's raw metrics. */
  explorable: boolean;
  reason?: SourceReason;
}

export interface PanelInfo {
  id: string;
  title: string;
  unit: MetricUnit;
}

export interface Dashboard {
  source: MetricsSource;
  panels: PanelInfo[];
}

export interface MetricPoint {
  /** Unix milliseconds. */
  t: number;
  /** Null when the source has no finite value. */
  v: number | null;
}

export interface MetricSeries {
  name: string;
  points: MetricPoint[];
}

export interface PanelData {
  series: MetricSeries[];
}

export type MetricType = 'counter' | 'gauge' | 'histogram' | 'summary' | 'unknown';

export interface MetricInfo {
  name: string;
  type: MetricType;
  help?: string;
}

export interface MetricCatalog {
  metrics: MetricInfo[];
}

export interface Exploration {
  unit: MetricUnit;
  series: MetricSeries[];
}
