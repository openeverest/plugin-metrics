import { QueryClient } from '@tanstack/react-query';
import type {
  ClusterDetailTabProps,
  PluginRegisterFn,
} from '@openeverest/plugin-sdk';
import { MetricsTab } from 'components/metrics-tab/metrics-tab';
import { Messages } from 'components/metrics-tab/metrics-tab.messages';
import { PluginRoot } from 'components/plugin-root/plugin-root';

const TAB_PATH = 'metrics';

const register: PluginRegisterFn = (api) => {
  const queryClient = new QueryClient();

  const MetricsTabExtension = ({ namespace, instanceName }: ClusterDetailTabProps) => (
    <PluginRoot api={api} queryClient={queryClient}>
      <MetricsTab namespace={namespace} instanceName={instanceName} />
    </PluginRoot>
  );

  api.registerExtension({
    type: 'clusterDetailTab',
    label: Messages.tabLabel,
    path: TAB_PATH,
    component: MetricsTabExtension,
  });
};

export default register;
