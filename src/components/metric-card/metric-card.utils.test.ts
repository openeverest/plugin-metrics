import { describe, expect, it } from 'vitest';
import { byteTicks, formatValue, headlineSeries, toChartData } from './metric-card.utils';

const LIMIT_COLOR = 'grey';

describe('toChartData', () => {
  it('aligns series on shared timestamps and keeps gaps as null', () => {
    const chart = toChartData(
      [
        { name: 'proxy', points: [{ t: 2000, v: 2 }, { t: 1000, v: 1 }] },
        { name: 'dataNode', points: [{ t: 3000, v: 3 }, { t: 1000, v: null }] },
      ],
      'CPU',
      LIMIT_COLOR
    );

    expect(chart.timestamps.map((d) => d.getTime())).toEqual([1000, 2000, 3000]);
    expect(chart.series.map(({ label, data, latest }) => ({ label, data, latest }))).toEqual([
      { label: 'proxy', data: [1, 2, null], latest: 2 },
      { label: 'dataNode', data: [null, null, 3], latest: 3 },
    ]);
    expect(chart.series[0].color).not.toBe(chart.series[1].color);
  });

  it('names an unlabelled series after the panel', () => {
    expect(toChartData([{ name: '', points: [] }], 'Latency', LIMIT_COLOR).series[0].label).toBe(
      'Latency'
    );
  });

  it('draws limits in the limit color without taking a palette color', () => {
    const points = [{ t: 1000, v: 1 }];
    const chart = toChartData(
      [
        { name: 'orders-0', points },
        { name: 'limit', kind: 'limit', points },
        { name: 'orders-1', points },
      ],
      'CPU',
      LIMIT_COLOR
    );
    const [first, limit, second] = chart.series;

    expect(limit).toMatchObject({ isLimit: true, color: LIMIT_COLOR });
    expect([first.isLimit, second.isLimit]).toEqual([false, false]);
    expect(second.color).toBe(
      toChartData([{ name: 'a', points }, { name: 'b', points }], 'CPU', LIMIT_COLOR).series[1].color
    );
  });
});

describe('headlineSeries', () => {
  const points = [{ t: 1000, v: 1 }];

  it('headlines a single measurement even next to its limit', () => {
    const chart = toChartData(
      [
        { name: 'orders-0', points },
        { name: 'limit', kind: 'limit', points },
      ],
      'CPU',
      LIMIT_COLOR
    );
    expect(headlineSeries(chart)?.label).toBe('orders-0');
  });

  it('headlines nothing when there are several measurements', () => {
    const chart = toChartData(
      [
        { name: 'orders-0', points },
        { name: 'orders-1', points },
      ],
      'CPU',
      LIMIT_COLOR
    );
    expect(headlineSeries(chart)).toBeNull();
  });
});

describe('byteTicks', () => {
  it('steps in round binary units', () => {
    const mib = 1024 ** 2;
    expect(byteTicks(540 * mib)).toEqual([0, 200 * mib, 400 * mib, 600 * mib]);
    expect(byteTicks(3.2 * 1024 ** 3)).toEqual([0, 1, 2, 3, 4].map((g) => g * 1024 ** 3));
  });

  it('leaves an empty axis to the chart', () => {
    expect(byteTicks(0)).toBeUndefined();
  });
});

describe('formatValue', () => {
  it.each([
    [1536, 'bytes', '1.5 KiB'],
    [3 * 1024 ** 3, 'bytes', '3 GiB'],
    [2048, 'Bps', '2 KiB/s'],
    [12.345, 'ms', '12.35 ms'],
    [2500, 'ms', '2.5 s'],
    [1.9, 'ops', '1.9/s'],
    [0.0123, 'cores', '0.012 cores'],
    [42, '', '42'],
  ] as const)('formats %s %s as %s', (value, unit, expected) => {
    expect(formatValue(value, unit)).toBe(expected);
  });
});
