import { useMemo } from 'react';
import { Alert, CircularProgress, Paper, Stack, Typography } from '@mui/material';
import { LineChart } from '@mui/x-charts/LineChart';
import { InstanceTarget } from 'api/metrics-api';
import { usePanelData } from 'hooks/usePanelData';
import { PanelInfo, TimeRange } from 'types/metrics.types';
import { CHART_HEIGHT } from './metric-panel.constants';
import { Messages } from './metric-panel.messages';
import { formatTime, formatValue, toChartData } from './metric-panel.utils';

interface MetricPanelProps {
  target: InstanceTarget;
  panel: PanelInfo;
  range: TimeRange;
}

export const MetricPanel = ({ target, panel, range }: MetricPanelProps) => {
  const { data, isLoading, error } = usePanelData(target, panel.id, range);
  const chart = useMemo(() => toChartData(data?.series ?? [], panel.title), [data, panel.title]);
  const formatSample = (value: number | null) => (value === null ? '' : formatValue(value, panel.unit));

  const body = () => {
    if (isLoading) {
      return <CircularProgress size={24} sx={{ alignSelf: 'center' }} />;
    }
    if (error) {
      return <Alert severity="error">{Messages.loadFailed(error.message)}</Alert>;
    }
    if (chart.series.length === 0) {
      return (
        <Typography variant="body2" color="text.secondary" sx={{ alignSelf: 'center' }}>
          {Messages.noData}
        </Typography>
      );
    }
    return (
      <LineChart
        height={CHART_HEIGHT}
        skipAnimation
        hideLegend={chart.series.length < 2}
        xAxis={[{ data: chart.timestamps, scaleType: 'time', valueFormatter: formatTime }]}
        yAxis={[{ valueFormatter: (value: number) => formatValue(value, panel.unit), width: 80 }]}
        series={chart.series.map((s) => ({
          label: s.label,
          data: s.data,
          showMark: false,
          valueFormatter: formatSample,
        }))}
      />
    );
  };

  return (
    <Paper variant="outlined" sx={{ p: 2 }}>
      <Typography variant="subtitle2">{panel.title}</Typography>
      <Stack sx={{ minHeight: CHART_HEIGHT, justifyContent: 'center' }}>
        {body()}
      </Stack>
    </Paper>
  );
};
