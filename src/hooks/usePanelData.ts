import { useQuery } from '@tanstack/react-query';
import { getPanelData, InstanceTarget } from 'api/metrics-api';
import { usePluginApi } from 'components/plugin-api-context/plugin-api.context';
import { TimeRange } from 'types/metrics.types';
import { RANGE_REFETCH_INTERVAL_MS } from './refetch.constants';

export const usePanelData = (target: InstanceTarget, panelId: string, range: TimeRange) => {
  const api = usePluginApi();
  return useQuery({
    queryKey: ['metrics-panel', target.namespace, target.instanceName, panelId, range],
    queryFn: () => getPanelData(api.fetch, target, panelId, range),
    refetchInterval: RANGE_REFETCH_INTERVAL_MS[range],
  });
};
