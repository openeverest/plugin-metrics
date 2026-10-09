import { useQuery } from '@tanstack/react-query';
import { getDashboard, InstanceTarget } from 'api/metrics-api';
import { usePluginApi } from 'components/plugin-api-context/plugin-api.context';

// Picks up the instance's monitoring being switched on or off.
const REFETCH_INTERVAL_MS = 60_000;

export const useDashboard = (target: InstanceTarget) => {
  const api = usePluginApi();
  return useQuery({
    queryKey: ['metrics-dashboard', target.namespace, target.instanceName],
    queryFn: () => getDashboard(api.fetch, target),
    refetchInterval: REFETCH_INTERVAL_MS,
  });
};
