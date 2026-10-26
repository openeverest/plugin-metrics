import { describe, expect, it } from 'vitest';
import { Dashboard, MetricsSource, PanelInfo } from 'types/metrics.types';
import { Messages } from './metrics-tab.messages';
import { dashboardNotice, panelCaption } from './metrics-tab.utils';

const podCpu: PanelInfo = { id: 'pod-cpu', title: 'CPU usage', unit: 'cores', source: 'prometheus' };
const qps: PanelInfo = { id: 'qps', title: 'Queries', unit: 'ops' };
const pmm: MetricsSource = { type: 'pmm', enabled: true, explorable: false };

const dashboard = (source: MetricsSource, panels: PanelInfo[]): Dashboard => ({ source, panels });

describe('dashboardNotice', () => {
  it('says why the engine source is off, even when pod panels show', () => {
    const off: MetricsSource = { type: 'pmm', enabled: false, explorable: false, reason: 'notEnabled' };
    expect(dashboardNotice(dashboard(off, [podCpu]))).toBe(Messages.sourceUnavailable.notEnabled('pmm'));
  });

  it('says the engine has no dashboard when only pod panels show', () => {
    expect(dashboardNotice(dashboard(pmm, [podCpu]))).toBe(Messages.noDashboard);
    expect(dashboardNotice(dashboard({ ...pmm, explorable: true }, [podCpu]))).toBeNull();
  });

  it('says nothing when the engine has panels', () => {
    expect(dashboardNotice(dashboard(pmm, [podCpu, qps]))).toBeNull();
  });
});

describe('panelCaption', () => {
  it('names a panel source only when the dashboard does not already show it', () => {
    expect(panelCaption(podCpu, pmm)).toBe(Messages.source('prometheus'));
    expect(panelCaption(podCpu, { ...pmm, type: 'prometheus' })).toBeUndefined();
    expect(panelCaption(podCpu, { ...pmm, type: 'prometheus', enabled: false })).toBe(
      Messages.source('prometheus')
    );
    expect(panelCaption(qps, pmm)).toBeUndefined();
  });
});
