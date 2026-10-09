import { useState } from 'react';
import { Box, Stack } from '@mui/material';
import { InstanceTarget } from 'api/metrics-api';
import { PANEL_GRID_SX } from 'components/metrics-tab/metrics-tab.constants';
import { useMetricCatalog } from 'hooks/useMetricCatalog';
import { MetricInfo, TimeRange } from 'types/metrics.types';
import { ExploredMetric } from './explored-metric/explored-metric';
import { MetricPicker } from './metric-picker/metric-picker';

interface MetricExplorerProps {
  target: InstanceTarget;
  range: TimeRange;
}

// Picked metrics live only in this component's state: a quick look, not a saved dashboard.
export const MetricExplorer = ({ target, range }: MetricExplorerProps) => {
  const { data, isLoading, error } = useMetricCatalog(target);
  const [picked, setPicked] = useState<MetricInfo[]>([]);

  return (
    <Stack sx={{ gap: 2 }}>
      <MetricPicker
        metrics={data?.metrics ?? []}
        picked={picked}
        isLoading={isLoading}
        error={error}
        onPick={(metric) => setPicked((current) => [metric, ...current])}
      />
      {picked.length > 0 && (
        <Box sx={PANEL_GRID_SX}>
          {picked.map((metric) => (
            <ExploredMetric
              key={metric.name}
              target={target}
              metric={metric}
              range={range}
              onRemove={() => setPicked((current) => current.filter((m) => m.name !== metric.name))}
            />
          ))}
        </Box>
      )}
    </Stack>
  );
};
