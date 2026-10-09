import type { PluginApi } from '@openeverest/plugin-sdk';
import { Dashboard, PanelData, TimeRange } from 'types/metrics.types';

type PluginFetch = PluginApi['fetch'];

export interface InstanceTarget {
  namespace: string;
  instanceName: string;
}

const instanceParams = ({ namespace, instanceName }: InstanceTarget) =>
  new URLSearchParams({ namespace, instance: instanceName });

const errorFromResponse = async (response: Response) => {
  const body = await response.json().catch(() => null);
  const message =
    body && typeof body.error === 'string'
      ? body.error
      : `HTTP ${response.status}`;
  return new Error(message);
};

export const getDashboard = async (
  pluginFetch: PluginFetch,
  target: InstanceTarget
): Promise<Dashboard> => {
  const response = await pluginFetch(`/api/dashboard?${instanceParams(target)}`);
  if (!response.ok) {
    throw await errorFromResponse(response);
  }
  return response.json();
};

export const getPanelData = async (
  pluginFetch: PluginFetch,
  target: InstanceTarget,
  panelId: string,
  range: TimeRange
): Promise<PanelData> => {
  const params = instanceParams(target);
  params.set('range', range);
  const response = await pluginFetch(
    `/api/panels/${encodeURIComponent(panelId)}?${params}`
  );
  if (!response.ok) {
    throw await errorFromResponse(response);
  }
  return response.json();
};
