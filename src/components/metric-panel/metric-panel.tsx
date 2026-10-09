import { useMemo } from 'react';
import { Alert, Paper, Skeleton, Stack, Typography } from '@mui/material';
import { InstanceTarget } from 'api/metrics-api';
import { usePanelData } from 'hooks/usePanelData';
import { PanelInfo, TimeRange } from 'types/metrics.types';
import { MetricChart } from './metric-chart/metric-chart';
import { CHART_HEIGHT } from './metric-panel.constants';
import { Messages } from './metric-panel.messages';
import { formatValue, toChartData } from './metric-panel.utils';
import { SeriesLegend } from './series-legend/series-legend';

interface MetricPanelProps {
  target: InstanceTarget;
  panel: PanelInfo;
  range: TimeRange;
}

export const MetricPanel = ({ target, panel, range }: MetricPanelProps) => {
  const { data, isLoading, error } = usePanelData(target, panel.id, range);
  const chart = useMemo(() => toChartData(data?.series ?? [], panel.title), [data, panel.title]);
  const single = chart.series.length === 1 ? chart.series[0] : null;

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
        <MetricChart chart={chart} unit={panel.unit} />
        {!single && <SeriesLegend series={chart.series} unit={panel.unit} />}
      </>
    );
  };

  return (
    <Paper variant="outlined" sx={{ p: 2.5, borderRadius: 3 }}>
      <Stack sx={{ gap: 1.5 }}>
        <Stack sx={{ gap: 0.25 }}>
          <Typography variant="body2" color="text.secondary" sx={{ fontWeight: 500 }}>
            {panel.title}
          </Typography>
          {single?.latest != null && (
            <Typography variant="h5" sx={{ fontWeight: 600, fontVariantNumeric: 'tabular-nums' }}>
              {formatValue(single.latest, panel.unit)}
            </Typography>
          )}
        </Stack>
        {body()}
      </Stack>
    </Paper>
  );
};
