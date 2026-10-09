import { useQuery } from '@tanstack/react-query';
import { getPanelData, InstanceTarget } from 'api/metrics-api';
import { usePluginApi } from 'components/plugin-api-context/plugin-api.context';
import { TimeRange } from 'types/metrics.types';

// Matches the backend step of each range, so every refresh adds a point.
const REFETCH_INTERVAL_MS: Record<TimeRange, number> = {
  '1h': 30_000,
  '24h': 300_000,
};

export const usePanelData = (target: InstanceTarget, panelId: string, range: TimeRange) => {
  const api = usePluginApi();
  return useQuery({
    queryKey: ['metrics-panel', target.namespace, target.instanceName, panelId, range],
    queryFn: () => getPanelData(api.fetch, target, panelId, range),
    refetchInterval: REFETCH_INTERVAL_MS[range],
  });
};
