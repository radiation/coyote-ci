import type { BuildStatus, BuildStepStatus } from '../types';

const STATUS_COLORS: Record<string, string> = {
  pending: '#888',
  queued: '#2196f3',
  running: '#ff9800',
  success: '#4caf50',
  failed: '#f44336',
};

export function StatusBadge({ status }: { status: BuildStatus | BuildStepStatus }) {
  return (
    <span
      style={{
        display: 'inline-block',
        padding: '2px 8px',
        borderRadius: '4px',
        fontSize: '0.8rem',
        fontWeight: 600,
        color: '#fff',
        backgroundColor: STATUS_COLORS[status] ?? '#888',
      }}
    >
      {status}
    </span>
  );
}
