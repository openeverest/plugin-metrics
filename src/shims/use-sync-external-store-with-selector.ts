// ESM port of use-sync-external-store/shim/with-selector (MIT, Meta). The upstream
// package is CommonJS and `require("react")`s, which can't reach the host React
// exposed through the import map, so @base-ui/utils (via @mui/x-charts) is aliased here.
import { useDebugValue, useEffect, useMemo, useRef, useSyncExternalStore } from 'react';

export function useSyncExternalStoreWithSelector<Snapshot, Selection>(
  subscribe: (onStoreChange: () => void) => () => void,
  getSnapshot: () => Snapshot,
  getServerSnapshot: undefined | null | (() => Snapshot),
  selector: (snapshot: Snapshot) => Selection,
  isEqual?: (a: Selection, b: Selection) => boolean
): Selection {
  // Last committed selection, reused when isEqual says the new one is the same.
  const committed = useRef<{ value: Selection } | null>(null);

  const [getSelection, getServerSelection] = useMemo(() => {
    let memo: { snapshot: Snapshot; selection: Selection } | null = null;

    const memoizedSelector = (nextSnapshot: Snapshot): Selection => {
      if (memo === null) {
        const nextSelection = selector(nextSnapshot);
        const current = committed.current;
        const selection =
          isEqual !== undefined && current !== null && isEqual(current.value, nextSelection)
            ? current.value
            : nextSelection;
        memo = { snapshot: nextSnapshot, selection };
        return selection;
      }
      if (Object.is(memo.snapshot, nextSnapshot)) {
        return memo.selection;
      }
      const nextSelection = selector(nextSnapshot);
      if (isEqual !== undefined && isEqual(memo.selection, nextSelection)) {
        memo.snapshot = nextSnapshot;
        return memo.selection;
      }
      memo = { snapshot: nextSnapshot, selection: nextSelection };
      return nextSelection;
    };

    return [
      () => memoizedSelector(getSnapshot()),
      getServerSnapshot ? () => memoizedSelector(getServerSnapshot()) : undefined,
    ];
  }, [getSnapshot, getServerSnapshot, selector, isEqual]);

  const value = useSyncExternalStore(subscribe, getSelection, getServerSelection);

  useEffect(() => {
    committed.current = { value };
  }, [value]);

  useDebugValue(value);
  return value;
}

// zustand consumes this module through a default import.
export default { useSyncExternalStoreWithSelector };
