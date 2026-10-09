import { describe, expect, it } from 'vitest';
import { MetricInfo } from 'types/metrics.types';
import { toPickerOptions } from './metric-explorer.utils';

const gauge = (name: string): MetricInfo => ({ name, type: 'gauge' });

describe('toPickerOptions', () => {
  it('splits large prefixes one level deeper and keeps small ones whole', () => {
    const milvus = Array.from({ length: 21 }, (_, i) => gauge(`milvus_proxy_m${i}`));
    const options = toPickerOptions(
      [gauge('process_open_fds'), gauge('process_cpu_seconds_total'), ...milvus],
      []
    );

    expect(new Set(options.map((o) => o.group))).toEqual(new Set(['milvus_proxy', 'process']));
    expect(options.at(-1)?.metric.name).toBe('process_open_fds');
  });

  it('hides metrics that are already shown', () => {
    const options = toPickerOptions([gauge('a_b'), gauge('a_c')], [gauge('a_b')]);
    expect(options.map((o) => o.metric.name)).toEqual(['a_c']);
  });

  it('folds one-metric groups into Other, listed last', () => {
    const options = toPickerOptions([gauge('exec_latency'), gauge('ann_init'), gauge('process_a'), gauge('process_b')], []);
    expect(options.map((o) => [o.group, o.metric.name])).toEqual([
      ['process', 'process_a'],
      ['process', 'process_b'],
      ['Other', 'ann_init'],
      ['Other', 'exec_latency'],
    ]);
  });
});
