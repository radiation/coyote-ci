import type { BuildStatus, BuildStepStatus } from "../types";

function statusLabel(status: BuildStatus | BuildStepStatus): string {
  return status.charAt(0).toUpperCase() + status.slice(1);
}

export function StatusBadge({
  status,
}: {
  status: BuildStatus | BuildStepStatus;
}) {
  return (
    <span className={`status-badge status-${status}`}>
      {statusLabel(status)}
    </span>
  );
}
