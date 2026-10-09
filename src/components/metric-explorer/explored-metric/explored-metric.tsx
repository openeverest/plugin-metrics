import { IconButton } from '@mui/material';
import CloseIcon from '@mui/icons-material/Close';
import { InstanceTarget } from 'api/metrics-api';
import { MetricCard } from 'components/metric-card/metric-card';
import { useExploredMetric } from 'hooks/useExploredMetric';
import { MetricInfo, TimeRange } from 'types/metrics.types';
import { Messages } from '../metric-explorer.messages';

interface ExploredMetricProps {
  target: InstanceTarget;
  metric: MetricInfo;
  range: TimeRange;
  onRemove: () => void;
}

export const ExploredMetric = ({ target, metric, range, onRemove }: ExploredMetricProps) => {
  const { data, isLoading, error } = useExploredMetric(target, metric, range);
  return (
    <MetricCard
      title={metric.name}
      caption={Messages.caption(metric.type, metric.help)}
      unit={data?.unit ?? ''}
      series={data?.series}
      isLoading={isLoading}
      error={error}
      action={
        <IconButton size="small" aria-label={Messages.remove} onClick={onRemove}>
          <CloseIcon fontSize="small" />
        </IconButton>
      }
    />
  );
};
