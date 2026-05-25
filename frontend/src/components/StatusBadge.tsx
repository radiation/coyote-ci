import type { BuildStatus, BuildStepStatus } from "../types/build";
import type { WorkerStatus } from "../types/worker";

function statusLabel(status: BuildStatus | BuildStepStatus): string {
  return status.charAt(0).toUpperCase() + status.slice(1);
}

export function StatusBadge({
  status,
}: {
  status: BuildStatus | BuildStepStatus | WorkerStatus;
}) {
  return (
    <span className={`status-badge status-${status}`}>
      {statusLabel(status)}
    </span>
  );
}
