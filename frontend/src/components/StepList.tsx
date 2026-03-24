import type { BuildStep } from '../types';
import { StatusBadge } from './StatusBadge';
import { formatTime } from './TimeDisplay';

export function StepList({ steps }: { steps: BuildStep[] }) {
  if (steps.length === 0) {
    return <p className="empty">No steps defined for this build.</p>;
  }

  return (
    <table className="table">
      <thead>
        <tr>
          <th>#</th>
          <th>Name</th>
          <th>Status</th>
          <th>Worker</th>
          <th>Started</th>
          <th>Ended</th>
          <th>Exit Code</th>
          <th>Error</th>
        </tr>
      </thead>
      <tbody>
        {steps.map((step) => (
          <tr key={step.step_index}>
            <td>{step.step_index}</td>
            <td>{step.name}</td>
            <td><StatusBadge status={step.status} /></td>
            <td>{step.worker_id ?? '—'}</td>
            <td>{formatTime(step.started_at)}</td>
            <td>{formatTime(step.finished_at)}</td>
            <td>{step.exit_code ?? '—'}</td>
            <td className="error-text">{step.error_message ?? ''}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}
