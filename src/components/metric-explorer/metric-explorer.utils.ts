import { MetricInfo } from 'types/metrics.types';
import { Messages } from './metric-explorer.messages';

export interface PickerOption {
  metric: MetricInfo;
  group: string;
}

// Above this many metrics a prefix is split one level deeper, so exporters with
// one namespace (milvus_*) still get useful groups (milvus_proxy, milvus_querynode).
const MAX_GROUP_SIZE = 20;

const prefix = (name: string, depth: number) => name.split('_').slice(0, depth).join('_');

const countBy = (values: string[]) => {
  const counts = new Map<string, number>();
  for (const value of values) {
    counts.set(value, (counts.get(value) ?? 0) + 1);
  }
  return counts;
};

// Options sorted by group, as the picker's grouping requires; one-metric groups
// are folded into "Other", which sorts last.
export const toPickerOptions = (metrics: MetricInfo[], picked: MetricInfo[]): PickerOption[] => {
  const pickedNames = new Set(picked.map((m) => m.name));
  const topLevelSize = countBy(metrics.map(({ name }) => prefix(name, 1)));
  const groupOf = (name: string) => {
    const top = prefix(name, 1);
    const deep = (topLevelSize.get(top) ?? 0) > MAX_GROUP_SIZE && name.split('_').length > 2;
    return deep ? prefix(name, 2) : top;
  };
  const groupSize = countBy(metrics.map(({ name }) => groupOf(name)));

  return metrics
    .filter((m) => !pickedNames.has(m.name))
    .map((metric) => {
      const group = groupOf(metric.name);
      return { metric, group: (groupSize.get(group) ?? 0) > 1 ? group : Messages.otherGroup };
    })
    .sort(
      (a, b) =>
        Number(a.group === Messages.otherGroup) - Number(b.group === Messages.otherGroup) ||
        a.group.localeCompare(b.group) ||
        a.metric.name.localeCompare(b.metric.name)
    );
};
