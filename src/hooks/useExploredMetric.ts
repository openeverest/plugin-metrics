import { useQuery } from '@tanstack/react-query';
import { exploreMetric, InstanceTarget } from 'api/metrics-api';
import { usePluginApi } from 'components/plugin-api-context/plugin-api.context';
import { MetricInfo, TimeRange } from 'types/metrics.types';
import { RANGE_REFETCH_INTERVAL_MS } from './refetch.constants';

export const useExploredMetric = (target: InstanceTarget, metric: MetricInfo, range: TimeRange) => {
  const api = usePluginApi();
  return useQuery({
    queryKey: ['metrics-explore', target.namespace, target.instanceName, metric.name, metric.type, range],
    queryFn: () => exploreMetric(api.fetch, target, metric, range),
    refetchInterval: RANGE_REFETCH_INTERVAL_MS[range],
  });
};
