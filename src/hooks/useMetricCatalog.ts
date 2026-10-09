import { useQuery } from '@tanstack/react-query';
import { getMetricCatalog, InstanceTarget } from 'api/metrics-api';
import { usePluginApi } from 'components/plugin-api-context/plugin-api.context';

// New metrics appear only when an exporter starts recording them.
const STALE_TIME_MS = 5 * 60_000;

export const useMetricCatalog = (target: InstanceTarget) => {
  const api = usePluginApi();
  return useQuery({
    queryKey: ['metrics-catalog', target.namespace, target.instanceName],
    queryFn: () => getMetricCatalog(api.fetch, target),
    staleTime: STALE_TIME_MS,
  });
};
