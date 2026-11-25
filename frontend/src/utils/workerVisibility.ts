import type { Worker } from "../types/worker";

const STALE_WORKER_DISPLAY_RETENTION_MS = 24 * 60 * 60 * 1000;

export function shouldHideStaleWorker(worker: Worker, now: number): boolean {
  if (worker.status !== "stale" || worker.stale_lease) return false;

  const hasClaim = Boolean(
    worker.current_build_id ||
    typeof worker.current_build_number === "number" ||
    worker.current_step_id ||
    worker.current_step_name ||
    typeof worker.current_step_index === "number" ||
    worker.claimed_at ||
    worker.lease_expires_at ||
    worker.project_id ||
    worker.project_name ||
    worker.job_id ||
    worker.job_name,
  );
  if (hasClaim) return false;

  const heartbeat = Date.parse(worker.last_heartbeat_at ?? "");
  return (
    Number.isNaN(heartbeat) ||
    now - heartbeat > STALE_WORKER_DISPLAY_RETENTION_MS
  );
}
