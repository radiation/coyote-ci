import { Fragment, useEffect, useState } from 'react';
import { buildStepLogStreamURL, getStepLogs } from '../api';
import type { BuildStep } from '../types';
import { StatusBadge } from './StatusBadge';
import { formatTime } from '../utils/time';

const COMMAND_PREVIEW_LIMIT = 72;

function commandPreview(command: string): string {
  if (command.length <= COMMAND_PREVIEW_LIMIT) {
    return command;
  }

  return `${command.slice(0, COMMAND_PREVIEW_LIMIT - 3)}...`;
}

type StepLogChunk = {
  sequence_no: number;
  stream: 'stdout' | 'stderr' | 'system';
  chunk_text: string;
  created_at: string;
};

export function StepList({ buildID, steps }: { buildID: string; steps: BuildStep[] }) {
  const [openStepIndex, setOpenStepIndex] = useState<number | null>(null);
  const [logChunks, setLogChunks] = useState<Record<number, StepLogChunk[]>>({});
  const [logLoading, setLogLoading] = useState<Record<number, boolean>>({});
  const [logError, setLogError] = useState<Record<number, string | null>>({});

  useEffect(() => {
    if (openStepIndex === null) {
      return;
    }

    let eventSource: EventSource | null = null;
    let reconnectTimer: number | null = null;
    let closed = false;

    const closeCurrent = () => {
      if (eventSource) {
        eventSource.close();
        eventSource = null;
      }
      if (reconnectTimer !== null) {
        window.clearTimeout(reconnectTimer);
        reconnectTimer = null;
      }
    };

    let latestSequence = 0;

    const connect = (after: number) => {
      if (closed) {
        return;
      }

      latestSequence = after;

      eventSource = new EventSource(buildStepLogStreamURL(buildID, openStepIndex, after));

      eventSource.addEventListener('chunk', (evt: MessageEvent) => {
        const parsed = JSON.parse(evt.data) as StepLogChunk;
        if (parsed.sequence_no <= latestSequence) {
          return;
        }
        latestSequence = Math.max(latestSequence, parsed.sequence_no);
        setLogChunks((prev) => {
          const existing = prev[openStepIndex] ?? [];
          return {
            ...prev,
            [openStepIndex]: [...existing, parsed],
          };
        });
      });

      eventSource.onerror = () => {
        closeCurrent();
        if (closed) {
          return;
        }
        reconnectTimer = window.setTimeout(() => {
          connect(latestSequence);
        }, 1500);
      };
    };

    const bootstrap = async () => {
      setLogLoading((prev) => ({ ...prev, [openStepIndex]: true }));
      setLogError((prev) => ({ ...prev, [openStepIndex]: null }));

      try {
        const history = await getStepLogs(buildID, openStepIndex, 0, 500);
        setLogChunks((prev) => ({ ...prev, [openStepIndex]: history.chunks }));
        latestSequence = history.next_sequence;
        connect(history.next_sequence);
      } catch (err) {
        setLogError((prev) => ({ ...prev, [openStepIndex]: String(err) }));
      } finally {
        setLogLoading((prev) => ({ ...prev, [openStepIndex]: false }));
      }
    };

    void bootstrap();

    return () => {
      closed = true;
      closeCurrent();
    };
  }, [buildID, openStepIndex]);

  if (steps.length === 0) {
    return <p className="empty">No steps defined for this build.</p>;
  }

  return (
    <table className="table">
      <thead>
        <tr>
          <th>#</th>
          <th>Name</th>
          <th>Command</th>
          <th>Status</th>
          <th>Job</th>
          <th>Worker</th>
          <th>Started</th>
          <th>Finished</th>
          <th>Exit Code</th>
          <th>Logs</th>
          <th>Error</th>
        </tr>
      </thead>
      <tbody>
        {steps.map((step) => {
          const isOpen = openStepIndex === step.step_index;
          const chunks = logChunks[step.step_index] ?? [];
          const loading = logLoading[step.step_index] ?? false;
          const error = logError[step.step_index];

          return (
            <Fragment key={`step-row-${step.step_index}`}>
              <tr key={step.step_index}>
                <td>{step.step_index}</td>
                <td>{step.name}</td>
                <td>
                  <code className="step-command" title={step.command}>{commandPreview(step.command)}</code>
                </td>
                <td><StatusBadge status={step.status} /></td>
                <td>{step.job ? <StatusBadge status={step.job.status} /> : '—'}</td>
                <td>{step.worker_id ?? '—'}</td>
                <td>{formatTime(step.started_at)}</td>
                <td>{formatTime(step.finished_at)}</td>
                <td>{step.exit_code ?? '—'}</td>
                <td>
                  <button
                    type="button"
                    className="logs-toggle"
                    onClick={() => setOpenStepIndex((prev) => (prev === step.step_index ? null : step.step_index))}
                  >
                    {isOpen ? 'Hide' : 'View'}
                  </button>
                </td>
                <td className="error-text">{step.error_message ?? '—'}</td>
              </tr>
              {isOpen && (
                <tr key={`logs-${step.step_index}`}>
                  <td colSpan={11}>
                    <div className="step-log-panel">
                      {step.job && (
                        <div className="detail-grid" style={{ marginBottom: '1rem' }}>
                          <div><strong>Job ID</strong><span>{step.job.id}</span></div>
                          <div><strong>Job Status</strong><span><StatusBadge status={step.job.status} /></span></div>
                          <div><strong>Image</strong><span>{step.job.image || '—'}</span></div>
                          <div><strong>Working Dir</strong><span>{step.job.working_dir || '—'}</span></div>
                          <div><strong>Command</strong><span>{step.job.command_preview || '—'}</span></div>
                          <div><strong>Timeout</strong><span>{step.job.timeout_seconds ?? '—'}</span></div>
                          <div><strong>Source Commit</strong><span>{step.job.source_commit_sha || '—'}</span></div>
                          <div><strong>Pipeline Path</strong><span>{step.job.pipeline_file_path || '—'}</span></div>
                          <div><strong>Context Dir</strong><span>{step.job.context_dir || '—'}</span></div>
                          <div><strong>Error</strong><span className="error-text">{step.job.error_message || '—'}</span></div>
                        </div>
                      )}

                      {step.job && (
                        <>
                          <strong>Outputs</strong>
                          {step.job.outputs.length === 0 && <p className="subtle-text">No declared or realized outputs.</p>}
                          {step.job.outputs.length > 0 && (
                            <table className="table">
                              <thead>
                                <tr>
                                  <th>Name</th>
                                  <th>Kind</th>
                                  <th>Declared Path</th>
                                  <th>Status</th>
                                  <th>URI</th>
                                </tr>
                              </thead>
                              <tbody>
                                {step.job.outputs.map((output) => (
                                  <tr key={output.id}>
                                    <td>{output.name}</td>
                                    <td>{output.kind}</td>
                                    <td>{output.declared_path}</td>
                                    <td>{output.status}</td>
                                    <td>{output.destination_uri ?? '—'}</td>
                                  </tr>
                                ))}
                              </tbody>
                            </table>
                          )}
                        </>
                      )}

                      {loading && <p className="subtle-text">Loading logs...</p>}
                      {error && <p className="error-text">Failed to load logs: {error}</p>}
                      {!loading && !error && chunks.length === 0 && <p className="subtle-text">No logs yet.</p>}
                      {!error && chunks.length > 0 && (
                        <pre className="step-log-pre">
                          {chunks.map((chunk) => `[${chunk.stream}] ${chunk.chunk_text}`).join('\n')}
                        </pre>
                      )}
                    </div>
                  </td>
                </tr>
              )}
            </Fragment>
          );
        })}
      </tbody>
    </table>
  );
}
