import { Dashboard, MetricsSource, PanelInfo } from 'types/metrics.types';
import { Messages } from './metrics-tab.messages';

/** Explains above the charts why some or all of them are missing; null when nothing is. */
export const dashboardNotice = ({ source, panels }: Dashboard): string | null => {
  if (!source.enabled) {
    return Messages.sourceUnavailable[source.reason ?? 'notEnabled'](source.type);
  }
  const hasEnginePanels = panels.some((panel) => !panel.source);
  return hasEnginePanels || source.explorable ? null : Messages.noDashboard;
};

/** Names the source of a panel that isn't read from the one shown for the dashboard. */
export const panelCaption = (panel: PanelInfo, source: MetricsSource): string | undefined =>
  panel.source && (!source.enabled || panel.source !== source.type)
    ? Messages.source(panel.source)
    : undefined;
