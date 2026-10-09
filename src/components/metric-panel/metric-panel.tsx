import { InstanceTarget } from 'api/metrics-api';
import { MetricCard } from 'components/metric-card/metric-card';
import { usePanelData } from 'hooks/usePanelData';
import { PanelInfo, TimeRange } from 'types/metrics.types';

interface MetricPanelProps {
  target: InstanceTarget;
  panel: PanelInfo;
  range: TimeRange;
}

export const MetricPanel = ({ target, panel, range }: MetricPanelProps) => {
  const { data, isLoading, error } = usePanelData(target, panel.id, range);
  return (
    <MetricCard
      title={panel.title}
      unit={panel.unit}
      series={data?.series}
      isLoading={isLoading}
      error={error}
    />
  );
};
