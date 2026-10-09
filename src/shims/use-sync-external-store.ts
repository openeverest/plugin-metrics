// The CommonJS use-sync-external-store/shim require()s React, which can't reach
// the host React behind the import map; React 18 ships the hook itself.
export { useSyncExternalStore } from 'react';
