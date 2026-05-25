import { useEffect, useState } from "react";
import { buildStepLogStreamURL, getStepLogs } from "../api";
import type { BuildStep } from "../types";
import { StatusBadge } from "./StatusBadge";
import { formatDuration, formatTime } from "../utils/time";

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
  activeStepIndex,
}: {
  buildID: string;
  steps: BuildStep[];
  activeStepIndex?: number;
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
    <div className="step-timeline">
      {buckets.map((bucket) => {
        const runningCount = bucket.steps.filter(
          (step) => step.status === "running",
        ).length;

        return (
          <section
            key={`bucket-${bucket.key}`}
            className="step-bucket"
            aria-label={
              bucket.groupName
                ? `Step group ${bucket.groupName}`
                : `Step bucket starting at step ${bucket.steps[0]?.step_index ?? 0}`
            }
          >
            {bucket.groupName ? (
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
            ) : null}

            <div className="step-bucket-list">
              {bucket.steps.map((step) => {
                const isOpen = openStepIndex === step.step_index;
                const chunks = logChunks[step.step_index] ?? [];
                const loading = logLoading[step.step_index] ?? false;
                const error = logError[step.step_index];
                const isCurrent = activeStepIndex === step.step_index;
                const duration = formatDuration(
                  step.started_at,
                  step.finished_at,
                );

                return (
                  <article
                    key={`step-card-${step.step_index}`}
                    id={`step-${step.step_index}`}
                    className={`step-card${step.status === "failed" ? " is-failed" : ""}${isCurrent ? " is-current" : ""}`}
                  >
                    <div className="step-card-rail" aria-hidden="true" />
                    <div className="step-card-body">
                      <div className="step-card-header">
                        <div>
                          <div className="step-card-kicker subtle-text">
                            Step {step.step_index}
                            {isCurrent ? " · Current step" : ""}
                          </div>
                          <h4>{step.name}</h4>
                        </div>
                        <StatusBadge status={step.status} />
                      </div>

                      <p className="subtle-text step-card-command-row">
                        <code className="step-command" title={step.command}>
                          {commandPreview(step.command)}
                        </code>
                      </p>

                      <div className="step-card-meta-grid subtle-text">
                        <span>Started {formatTime(step.started_at)}</span>
                        <span>Finished {formatTime(step.finished_at)}</span>
                        <span>Duration {duration}</span>
                        <span>Worker {step.worker_id ?? "—"}</span>
                        <span>Exit code {step.exit_code ?? "—"}</span>
                      </div>

                      {step.error_message ? (
                        <p className="step-card-error error-text">
                          {step.error_message}
                        </p>
                      ) : null}

                      <div className="detail-actions-row">
                        <button
                          type="button"
                          className="logs-toggle"
                          onClick={() =>
                            setOpenStepIndex((prev) =>
                              prev === step.step_index ? null : step.step_index,
                            )
                          }
                        >
                          {isOpen ? "Hide logs" : "Open logs"}
                        </button>
                      </div>

                      {isOpen ? (
                        <div className="step-log-panel">
                          <p className="step-log-heading">Logs</p>
                          {loading ? (
                            <p className="subtle-text">Loading logs...</p>
                          ) : null}
                          {error ? (
                            <p className="error-text">
                              Failed to load logs: {error}
                            </p>
                          ) : null}
                          {!loading && !error && chunks.length === 0 ? (
                            <p className="subtle-text">No logs yet.</p>
                          ) : null}
                          {!error && chunks.length > 0 ? (
                            <pre className="step-log-pre">
                              {chunks
                                .map(
                                  (chunk) =>
                                    `[${chunk.stream}] ${chunk.chunk_text}`,
                                )
                                .join("\n")}
                            </pre>
                          ) : null}
                        </div>
                      ) : null}
                    </div>
                  </article>
                );
              })}
            </div>
          </section>
        );
      })}
    </div>
  );
}
