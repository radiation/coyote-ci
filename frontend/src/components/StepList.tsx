import { Fragment, useEffect, useState } from "react";
import { buildStepLogStreamURL, getStepLogs } from "../api";
import type { BuildStep } from "../types";
import { StatusBadge } from "./StatusBadge";
import { formatTime } from "../utils/time";

const COMMAND_PREVIEW_LIMIT = 72;

function commandPreview(command: string): string {
  if (command.length <= COMMAND_PREVIEW_LIMIT) {
    return command;
  }

  return `${command.slice(0, COMMAND_PREVIEW_LIMIT - 3)}...`;
}

type StepLogChunk = {
  sequence_no: number;
  stream: "stdout" | "stderr" | "system";
  chunk_text: string;
  created_at: string;
};

type StepBucket = {
  key: string;
  groupName: string | null;
  steps: BuildStep[];
};

function bucketSteps(steps: BuildStep[]): StepBucket[] {
  const buckets: StepBucket[] = [];
  for (const step of steps) {
    const trimmedGroup = (step.group_name ?? "").trim();
    const groupName = trimmedGroup === "" ? null : trimmedGroup;
    const bucketIdentity = groupName ?? `ungrouped:${step.step_index}`;
    const previous = buckets[buckets.length - 1];
    if (previous && previous.groupName === groupName) {
      previous.steps.push(step);
      continue;
    }
    buckets.push({
      key: `${bucketIdentity}:${buckets.length}`,
      groupName,
      steps: [step],
    });
  }
  return buckets;
}

export function StepList({
  buildID,
  steps,
}: {
  buildID: string;
  steps: BuildStep[];
}) {
  const [openStepIndex, setOpenStepIndex] = useState<number | null>(null);
  const [logChunks, setLogChunks] = useState<Record<number, StepLogChunk[]>>(
    {},
  );
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

      eventSource = new EventSource(
        buildStepLogStreamURL(buildID, openStepIndex, after),
      );

      eventSource.addEventListener("chunk", (evt: MessageEvent) => {
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

  const buckets = bucketSteps(steps);

  return (
    <table className="table">
      <thead>
        <tr>
          <th>#</th>
          <th>Name</th>
          <th>Command</th>
          <th>Status</th>
          <th>Worker</th>
          <th>Started</th>
          <th>Finished</th>
          <th>Exit Code</th>
          <th>Logs</th>
          <th>Error</th>
        </tr>
      </thead>
      <tbody>
        {buckets.map((bucket) => {
          const runningCount = bucket.steps.filter(
            (step) => step.status === "running",
          ).length;

          return (
            <Fragment key={`bucket-${bucket.key}`}>
              {bucket.groupName && (
                <tr className="step-group-row">
                  <td colSpan={10}>
                    <div className="step-group-header">
                      <strong>{bucket.groupName}</strong>
                      <span className="step-group-meta">
                        {runningCount > 1
                          ? `${runningCount} steps running concurrently`
                          : runningCount === 1
                            ? "1 step running"
                            : `${bucket.steps.length} parallel steps`}
                      </span>
                    </div>
                  </td>
                </tr>
              )}

              {bucket.steps.map((step) => {
                const isOpen = openStepIndex === step.step_index;
                const chunks = logChunks[step.step_index] ?? [];
                const loading = logLoading[step.step_index] ?? false;
                const error = logError[step.step_index];

                return (
                  <Fragment key={`step-row-${step.step_index}`}>
                    <tr
                      className={
                        bucket.groupName ? "step-row-grouped" : undefined
                      }
                    >
                      <td>{step.step_index}</td>
                      <td>
                        <span
                          className={
                            bucket.groupName ? "step-name-grouped" : undefined
                          }
                        >
                          {step.name}
                        </span>
                      </td>
                      <td>
                        <code className="step-command" title={step.command}>
                          {commandPreview(step.command)}
                        </code>
                      </td>
                      <td>
                        <StatusBadge status={step.status} />
                      </td>
                      <td>{step.worker_id ?? "—"}</td>
                      <td>{formatTime(step.started_at)}</td>
                      <td>{formatTime(step.finished_at)}</td>
                      <td>{step.exit_code ?? "—"}</td>
                      <td>
                        <button
                          type="button"
                          className="logs-toggle"
                          onClick={() =>
                            setOpenStepIndex((prev) =>
                              prev === step.step_index ? null : step.step_index,
                            )
                          }
                        >
                          {isOpen ? "Hide" : "View"}
                        </button>
                      </td>
                      <td className="error-text">
                        {step.error_message ?? "—"}
                      </td>
                    </tr>
                    {isOpen && (
                      <tr key={`logs-${step.step_index}`}>
                        <td colSpan={10}>
                          <div className="step-log-panel">
                            {loading && (
                              <p className="subtle-text">Loading logs...</p>
                            )}
                            {error && (
                              <p className="error-text">
                                Failed to load logs: {error}
                              </p>
                            )}
                            {!loading && !error && chunks.length === 0 && (
                              <p className="subtle-text">No logs yet.</p>
                            )}
                            {!error && chunks.length > 0 && (
                              <pre className="step-log-pre">
                                {chunks
                                  .map(
                                    (chunk) =>
                                      `[${chunk.stream}] ${chunk.chunk_text}`,
                                  )
                                  .join("\n")}
                              </pre>
                            )}
                          </div>
                        </td>
                      </tr>
                    )}
                  </Fragment>
                );
              })}
            </Fragment>
          );
        })}
      </tbody>
    </table>
  );
}
