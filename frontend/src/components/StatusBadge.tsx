import type { BuildStatus, BuildStepStatus } from "../types/build";
import type { WorkerStatus } from "../types/worker";

type StatusBadgeStatus = BuildStatus | BuildStepStatus | WorkerStatus;

function statusLabel(status: StatusBadgeStatus): string {
  return status.charAt(0).toUpperCase() + status.slice(1);
}

export function StatusBadge({ status }: { status: StatusBadgeStatus }) {
  return (
    <span className={`status-badge status-${status}`}>
      {statusLabel(status)}
    </span>
  );
}
