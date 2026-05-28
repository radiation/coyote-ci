import type { BuildStatus } from "../types/build";

export const FAST_POLL_INTERVAL = 3000;
export const SLOW_POLL_INTERVAL = 15000;

/** Returns true when a build is still in progress (not yet terminal). */
export function isActiveBuild(status: BuildStatus | undefined): boolean {
  return Boolean(status) && !isTerminalBuild(status);
}

export function isTerminalBuild(status: BuildStatus | undefined): boolean {
  if (!status) return false;
  return (["success", "failed", "canceled"] as BuildStatus[]).includes(status);
}

export function isCancelableBuild(status: BuildStatus | undefined): boolean {
  return status === "queued" || status === "preparing" || status === "running";
}

export function isRerunnableBuild(status: BuildStatus | undefined): boolean {
  return isTerminalBuild(status);
}
