import { Link } from "react-router-dom";
import { StatusBadge } from "./StatusBadge";
import type { Build, QueueEntry } from "../types/build";
import { formatTime } from "../utils/time";

type BuildActivityContextMode = "global" | "project" | "job";

export type BuildActivityItem =
  { kind: "queue"; entry: QueueEntry } | { kind: "build"; build: Build };

type BuildActivityPanelProps = {
  title: string;
  items: BuildActivityItem[];
  emptyMessage: string;
  loadingMessage?: string;
  error?: unknown;
  errorPrefix?: string;
  contextMode?: BuildActivityContextMode;
};

function jobContextLabel(
  jobName: string | null | undefined,
  jobID: string | null | undefined,
): string | null {
  const nextJobName = jobName?.trim();
  if (nextJobName) {
    return nextJobName;
  }

  const nextJobID = jobID?.trim();
  if (!nextJobID) {
    return null;
  }

  return `Job ${nextJobID.slice(0, 8)}`;
}

function renderQueueContext(
  entry: QueueEntry,
  contextMode: BuildActivityContextMode,
) {
  const projectLabel =
    entry.project_name?.trim() ||
    entry.project_slug?.trim() ||
    entry.project_id;
  const hasJobID = Boolean(entry.job_id?.trim());
  const jobLabel = jobContextLabel(entry.job_name, entry.job_id);

  if (contextMode === "job") {
    return null;
  }

  if (contextMode === "project") {
    if (jobLabel && hasJobID) {
      return <Link to={`/jobs/${entry.job_id!}`}>{jobLabel}</Link>;
    }
    return jobLabel;
  }

  return (
    <>
      <Link to={`/projects/${entry.project_id}`}>{projectLabel}</Link>
      {jobLabel ? (hasJobID ? ` · ` : ` · ${jobLabel}`) : null}
      {jobLabel && hasJobID ? (
        <Link to={`/jobs/${entry.job_id!}`}>{jobLabel}</Link>
      ) : null}
    </>
  );
}

function renderBuildContext(
  build: Build,
  contextMode: BuildActivityContextMode,
) {
  const projectLabel =
    build.project_name?.trim() ||
    build.project_slug?.trim() ||
    build.project_id;
  const hasJobID = Boolean(build.job_id?.trim());
  const jobLabel = jobContextLabel(build.job_name, build.job_id);
  const triggerRef = build.trigger_ref?.trim();

  if (contextMode === "job") {
    return triggerRef ?? null;
  }

  if (contextMode === "project") {
    if (jobLabel && hasJobID) {
      return <Link to={`/jobs/${build.job_id!}`}>{jobLabel}</Link>;
    }
    return triggerRef ?? null;
  }

  return (
    <>
      <Link to={`/projects/${build.project_id}`}>{projectLabel}</Link>
      {jobLabel ? (hasJobID ? ` · ` : ` · ${jobLabel}`) : null}
      {jobLabel && hasJobID ? (
        <Link to={`/jobs/${build.job_id!}`}>{jobLabel}</Link>
      ) : null}
      {triggerRef ? ` · ${triggerRef}` : null}
    </>
  );
}

export function BuildActivityPanel({
  title,
  items,
  emptyMessage,
  loadingMessage,
  error,
  errorPrefix = "Failed to load activity",
  contextMode = "global",
}: BuildActivityPanelProps) {
  return (
    <section className="panel dashboard-panel">
      <div className="dashboard-panel-header">
        <h3>{title}</h3>
      </div>
      {loadingMessage ? (
        <p>{loadingMessage}</p>
      ) : error ? (
        <p className="error-text">
          {errorPrefix}: {String(error)}
        </p>
      ) : items.length === 0 ? (
        <div className="empty-state">
          <p className="empty">{emptyMessage}</p>
        </div>
      ) : (
        <ul className="activity-list">
          {items.map((item) => {
            if (item.kind === "queue") {
              const entry = item.entry;
              const context = renderQueueContext(entry, contextMode);
              return (
                <li
                  key={`queue-${entry.build_id}`}
                  className="activity-list-item"
                >
                  <div className="activity-list-main">
                    <div className="activity-list-title-row">
                      <Link to={`/builds/${entry.build_id}`}>
                        Build #{entry.build_number}
                      </Link>
                      <StatusBadge status={entry.status} />
                    </div>
                    {context ? <p className="subtle-text">{context}</p> : null}
                  </div>
                  <div className="activity-list-meta subtle-text">
                    {formatTime(
                      entry.started_at ?? entry.queued_at ?? entry.created_at,
                    )}
                  </div>
                </li>
              );
            }

            const build = item.build;
            const context = renderBuildContext(build, contextMode);
            return (
              <li
                key={`build-${build.id}`}
                className={`activity-list-item${build.status === "failed" ? " is-failed" : ""}`}
              >
                <div className="activity-list-main">
                  <div className="activity-list-title-row">
                    <Link to={`/builds/${build.id}`}>
                      Build{" "}
                      {build.build_number
                        ? `#${build.build_number}`
                        : build.id.slice(0, 8)}
                    </Link>
                    <StatusBadge status={build.status} />
                  </div>
                  {context ? <p className="subtle-text">{context}</p> : null}
                </div>
                <div className="activity-list-meta subtle-text">
                  {formatTime(
                    build.finished_at ??
                      build.started_at ??
                      build.queued_at ??
                      build.created_at,
                  )}
                </div>
              </li>
            );
          })}
        </ul>
      )}
    </section>
  );
}

export function BuildActivityList(props: BuildActivityPanelProps) {
  return <BuildActivityPanel {...props} />;
}
