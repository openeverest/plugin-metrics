import { useMemo, useState } from 'react';
import { Alert, Box, CircularProgress, Stack, Tab, Tabs, Typography } from '@mui/material';
import { MetricExplorer } from 'components/metric-explorer/metric-explorer';
import { MetricPanel } from 'components/metric-panel/metric-panel';
import { useDashboard } from 'hooks/useDashboard';
import { TimeRange } from 'types/metrics.types';
import { DEFAULT_TIME_RANGE, PANEL_GRID_SX, TIME_RANGES } from './metrics-tab.constants';
import { Messages } from './metrics-tab.messages';

interface MetricsTabProps {
  namespace: string;
  instanceName: string;
}

export const MetricsTab = ({ namespace, instanceName }: MetricsTabProps) => {
  const target = useMemo(() => ({ namespace, instanceName }), [namespace, instanceName]);
  const [range, setRange] = useState<TimeRange>(DEFAULT_TIME_RANGE);
  const { data: dashboard, isLoading, error } = useDashboard(target);

  if (isLoading) {
    return (
      <Box sx={{ display: 'flex', justifyContent: 'center', p: 4 }}>
        <CircularProgress />
      </Box>
    );
  }
  if (error || !dashboard) {
    return (
      <Alert severity="error" sx={{ mt: 2 }}>
        {Messages.loadFailed(error?.message ?? '')}
      </Alert>
    );
  }

  const { source, panels } = dashboard;
  if (!source.enabled) {
    return (
      <Alert severity="info" sx={{ mt: 2 }}>
        {Messages.sourceUnavailable[source.reason ?? 'notEnabled'](source.type)}
      </Alert>
    );
  }
  if (panels.length === 0 && !source.explorable) {
    return (
      <Alert severity="info" sx={{ mt: 2 }}>
        {Messages.noDashboard}
      </Alert>
    );
  }

  return (
    <Stack sx={{ gap: 2, mt: 2 }}>
      <Stack direction="row" sx={{ alignItems: 'center', justifyContent: 'space-between' }}>
        <Tabs value={range} onChange={(_, value: TimeRange) => setRange(value)}>
          {TIME_RANGES.map((r) => (
            <Tab key={r} value={r} label={Messages.timeRanges[r]} />
          ))}
        </Tabs>
        <Typography variant="caption" color="text.secondary">
          {Messages.source(source.type)}
        </Typography>
      </Stack>
      {source.explorable && <MetricExplorer target={target} range={range} />}
      {panels.length > 0 && (
        <Box sx={PANEL_GRID_SX}>
          {panels.map((panel) => (
            <MetricPanel key={panel.id} target={target} panel={panel} range={range} />
          ))}
        </Box>
      )}
    </Stack>
  );
};
