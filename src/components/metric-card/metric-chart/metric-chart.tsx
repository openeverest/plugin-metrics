import { useId } from 'react';
import { useTheme } from '@mui/material';
import { chartsGridClasses } from '@mui/x-charts/ChartsGrid';
import { LineChart, lineClasses } from '@mui/x-charts/LineChart';
import { MetricUnit } from 'types/metrics.types';
import { AREA_OPACITY, CHART_HEIGHT, MAX_SERIES_FOR_STRONG_FILL, Y_TICK_COUNT } from '../metric-card.constants';
import { byteTicks, ChartData, formatTime, formatValue, seriesMax } from '../metric-card.utils';

interface MetricChartProps {
  chart: ChartData;
  unit: MetricUnit;
}

export const MetricChart = ({ chart, unit }: MetricChartProps) => {
  const theme = useTheme();
  // useId contains ':', which breaks url(#...) references.
  const gradientPrefix = `pm-gradient-${useId().replace(/[^a-zA-Z0-9-]/g, '')}`;
  const ticks = unit === 'bytes' ? byteTicks(seriesMax(chart.series)) : undefined;
  const tickLabelStyle = { fontSize: 11, fill: theme.palette.text.secondary };
  const formatSample = (value: number | null) => (value === null ? '' : formatValue(value, unit));
  const areaOpacity =
    chart.series.length > MAX_SERIES_FOR_STRONG_FILL ? AREA_OPACITY.many : AREA_OPACITY.few;

  const seriesFills = Object.fromEntries(
    chart.series.map((_, i) => [
      `& .${lineClasses.area}[data-series="s${i}"]`,
      { fill: `url(#${gradientPrefix}-${i})` },
    ])
  );

  return (
    <LineChart
      height={CHART_HEIGHT}
      skipAnimation
      hideLegend
      grid={{ horizontal: true }}
      margin={{ top: 8, right: 8, bottom: 0, left: 0 }}
      xAxis={[
        {
          data: chart.timestamps,
          scaleType: 'time',
          valueFormatter: formatTime,
          disableLine: true,
          disableTicks: true,
          tickNumber: 5,
          tickLabelStyle,
        },
      ]}
      yAxis={[
        {
          min: 0,
          max: ticks?.[ticks.length - 1],
          tickInterval: ticks,
          tickNumber: Y_TICK_COUNT,
          valueFormatter: (value: number) => formatValue(value, unit),
          disableLine: true,
          disableTicks: true,
          width: 72,
          tickLabelStyle,
        },
      ]}
      series={chart.series.map((s, i) => ({
        id: `s${i}`,
        label: s.label,
        data: s.data,
        color: s.color,
        area: true,
        curve: 'monotoneX',
        showMark: false,
        valueFormatter: formatSample,
        highlightScope: { highlight: 'series', fade: 'global' },
      }))}
      sx={{
        [`& .${lineClasses.line}`]: { strokeWidth: 2, strokeLinejoin: 'round' },
        [`& .${chartsGridClasses.line}`]: {
          stroke: theme.palette.divider,
          strokeDasharray: '3 4',
        },
        ...seriesFills,
      }}
    >
      <defs>
        {chart.series.map((s, i) => (
          <linearGradient key={s.label} id={`${gradientPrefix}-${i}`} x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor={s.color} stopOpacity={areaOpacity} />
            <stop offset="100%" stopColor={s.color} stopOpacity={0} />
          </linearGradient>
        ))}
      </defs>
    </LineChart>
  );
};
