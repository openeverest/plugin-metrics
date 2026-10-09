import { describe, expect, it } from 'vitest';
import { formatValue, toChartData } from './metric-panel.utils';

describe('toChartData', () => {
  it('aligns series on shared timestamps and keeps gaps as null', () => {
    const chart = toChartData(
      [
        { name: 'proxy', points: [{ t: 2000, v: 2 }, { t: 1000, v: 1 }] },
        { name: 'dataNode', points: [{ t: 3000, v: 3 }, { t: 1000, v: null }] },
      ],
      'CPU'
    );

    expect(chart.timestamps.map((d) => d.getTime())).toEqual([1000, 2000, 3000]);
    expect(chart.series).toEqual([
      { label: 'proxy', data: [1, 2, null] },
      { label: 'dataNode', data: [null, null, 3] },
    ]);
  });

  it('names an unlabelled series after the panel', () => {
    expect(toChartData([{ name: '', points: [] }], 'Latency').series[0].label).toBe('Latency');
  });
});

describe('formatValue', () => {
  it.each([
    [1536, 'bytes', '1.5 KiB'],
    [3 * 1024 ** 3, 'bytes', '3 GiB'],
    [12.345, 'ms', '12.35 ms'],
    [2500, 'ms', '2.5 s'],
    [1.9, 'ops', '1.9/s'],
    [0.0123, 'cores', '0.012 cores'],
    [42, '', '42'],
  ] as const)('formats %s %s as %s', (value, unit, expected) => {
    expect(formatValue(value, unit)).toBe(expected);
  });
});
