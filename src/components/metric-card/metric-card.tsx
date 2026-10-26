import { ReactNode, useMemo } from 'react';
import { Alert, Paper, Skeleton, Stack, Typography, useTheme } from '@mui/material';
import { MetricSeries, MetricUnit } from 'types/metrics.types';
import { MetricChart } from './metric-chart/metric-chart';
import { CHART_HEIGHT } from './metric-card.constants';
import { Messages } from './metric-card.messages';
import { formatValue, headlineSeries, toChartData } from './metric-card.utils';
import { SeriesLegend } from './series-legend/series-legend';

interface MetricCardProps {
  title: string;
  caption?: string;
  unit: MetricUnit;
  series: MetricSeries[] | undefined;
  isLoading: boolean;
  error: Error | null;
  action?: ReactNode;
}

export const MetricCard = ({ title, caption, unit, series, isLoading, error, action }: MetricCardProps) => {
  const theme = useTheme();
  const limitColor = theme.palette.text.secondary;
  const chart = useMemo(
    () => toChartData(series ?? [], title, limitColor),
    [series, title, limitColor]
  );
  const single = headlineSeries(chart);

  const body = () => {
    if (isLoading) {
      return <Skeleton variant="rounded" height={CHART_HEIGHT} />;
    }
    if (error) {
      return <Alert severity="error">{Messages.loadFailed(error.message)}</Alert>;
    }
    if (chart.series.length === 0) {
      return (
        <Stack sx={{ height: CHART_HEIGHT, alignItems: 'center', justifyContent: 'center' }}>
          <Typography variant="body2" color="text.secondary">
            {Messages.noData}
          </Typography>
        </Stack>
      );
    }
    return (
      <>
        <MetricChart chart={chart} unit={unit} />
        {chart.series.length > 1 && <SeriesLegend series={chart.series} unit={unit} />}
      </>
    );
  };

  return (
    <Paper variant="outlined" sx={{ p: 2.5, borderRadius: 3, minWidth: 0 }}>
      <Stack sx={{ gap: 1.5 }}>
        <Stack direction="row" sx={{ alignItems: 'flex-start', gap: 1 }}>
          <Stack sx={{ gap: 0.25, flex: 1, minWidth: 0 }}>
            <Typography variant="body2" color="text.secondary" noWrap title={title} sx={{ fontWeight: 500 }}>
              {title}
            </Typography>
            {caption && (
              <Typography variant="caption" color="text.secondary" noWrap title={caption}>
                {caption}
              </Typography>
            )}
            {single?.latest != null && (
              <Typography variant="h5" sx={{ fontWeight: 600, fontVariantNumeric: 'tabular-nums' }}>
                {formatValue(single.latest, unit)}
              </Typography>
            )}
          </Stack>
          {action}
        </Stack>
        {body()}
      </Stack>
    </Paper>
  );
};
