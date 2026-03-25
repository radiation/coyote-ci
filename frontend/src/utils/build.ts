import type { BuildStatus } from '../types/build';

export const FAST_POLL_INTERVAL = 3000;
export const SLOW_POLL_INTERVAL = 15000;

/** Returns true when a build is still in progress (not yet terminal). */
export function isActiveBuild(status: BuildStatus | undefined): boolean {
  if (!status) return false;
  return !(['success', 'failed'] as BuildStatus[]).includes(status);
}
