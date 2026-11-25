import { useQuery, type UseQueryResult } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { APIError, listBuilds, listQueue, listWorkers } from "../api";
import { BuildIdentityLink } from "../components/BuildIdentityLink";
import { PageHeader } from "../components/PageHeader";
import { StatusBadge } from "../components/StatusBadge";
import type { Build, QueueEntry } from "../types/build";
import { FAST_POLL_INTERVAL, SLOW_POLL_INTERVAL } from "../utils/build";
import { formatTime } from "../utils/time";
import { shouldHideStaleWorker } from "../utils/workerVisibility";

const HISTORY_LIMIT = 24;
const historyURL = `/builds?limit=${HISTORY_LIMIT}&offset=0`;

function denied(error: unknown) {
  return (
    error instanceof APIError && (error.status === 401 || error.status === 403)
  );
}

function retry(count: number, error: Error) {
  return !denied(error) && count < 1;
}

function snapshot<T>(query: UseQueryResult<T>) {
  return denied(query.error) ? undefined : query.data;
}

function SnapshotNote<T>({
  query,
  source,
}: {
  query: UseQueryResult<T>;
  source: string;
}) {
  if (denied(query.error))
    return <p role="alert">Access to {source} is unavailable.</p>;
  const refreshed = formatTime(
    query.dataUpdatedAt ? new Date(query.dataUpdatedAt).toISOString() : null,
  );
  if (query.error)
    return (
      <p role="alert">
        Unable to refresh {source}.
        {query.data ? ` Last known results · refreshed ${refreshed}` : ""}
      </p>
    );
  if (!query.data) return <p role="status">Loading {source}…</p>;
  return <p className="subtle-text">Last refreshed {refreshed}</p>;
}

function eventOf(row: Build | QueueEntry) {
  if ("finished_at" in row && row.finished_at)
    return { label: "Finished", at: row.finished_at };
  if (row.started_at) return { label: "Started", at: row.started_at };
  if (row.queued_at) return { label: "Queued", at: row.queued_at };
  return { label: "Created", at: row.created_at };
}

function ExecutionList({ rows }: { rows: (Build | QueueEntry)[] }) {
  return (
    <ul className="activity-list">
      {rows.map((row) => {
        const id = "build_id" in row ? row.build_id : row.id;
        const event = eventOf(row);
        return (
          <li key={id} className="activity-list-item">
            <div className="activity-list-main">
              <div className="overview-identity">
                <BuildIdentityLink id={id} number={row.build_number} />
                <StatusBadge status={row.status} />
              </div>
              <div className="subtle-text">
                <Link to={`/projects/${row.project_id}`}>
                  {row.project_name || row.project_slug || row.project_id}
                </Link>
                {row.job_id && (
                  <>
                    {" "}
                    ·{" "}
                    <Link to={`/jobs/${row.job_id}`}>
                      {row.job_name || row.job_id}
                    </Link>
                  </>
                )}
              </div>
            </div>
            <span className="activity-list-meta">
              {event.label}{" "}
              <time dateTime={event.at}>{formatTime(event.at)}</time>
            </span>
          </li>
        );
      })}
    </ul>
  );
}

export function DashboardPage() {
  const queue = useQuery({
    queryKey: ["queue", "", undefined],
    queryFn: () => listQueue(),
    retry,
    refetchInterval: ({ state }) =>
      denied(state.error)
        ? false
        : state.data?.length
          ? FAST_POLL_INTERVAL
          : SLOW_POLL_INTERVAL,
  });
  const history = useQuery({
    queryKey: ["queue-page", "recent-builds", ""],
    queryFn: () => listBuilds({ limit: HISTORY_LIMIT, offset: 0 }),
    retry,
    refetchInterval: ({ state }) =>
      denied(state.error) ? false : SLOW_POLL_INTERVAL,
  });
  const workers = useQuery({
    queryKey: ["workers"],
    queryFn: listWorkers,
    retry,
    refetchInterval: ({ state }) =>
      denied(state.error) ? false : SLOW_POLL_INTERVAL,
  });

  const entries = snapshot(queue);
  const running = entries?.filter((entry) => entry.status === "running") ?? [];
  const runningPreview = running.slice(0, 3);
  const builds = snapshot(history);
  const recent = [...(builds ?? [])].sort(
    (left, right) =>
      Date.parse(eventOf(right).at) - Date.parse(eventOf(left).at),
  );
  const failed = recent.filter((build) => build.status === "failed");
  const failures = failed.slice(0, 3);
  const shown = new Set([
    ...runningPreview.map((entry) => entry.build_id),
    ...failures.map((build) => build.id),
  ]);
  const activity = recent.filter((build) => !shown.has(build.id)).slice(0, 4);
  const reported = snapshot(workers);
  const visible =
    reported?.filter(
      (worker) => !shouldHideStaleWorker(worker, workers.dataUpdatedAt),
    ) ?? [];
  const hiddenCount = (reported?.length ?? 0) - visible.length;

  return (
    <div className="page-content page-dashboard">
      <PageHeader
        title="Overview"
        actions={
          <Link className="secondary-button" to="/projects">
            View all projects
          </Link>
        }
      />

      <section
        className="panel dashboard-panel"
        aria-labelledby="overview-active"
      >
        <h3 id="overview-active">Active work</h3>
        <SnapshotNote query={queue} source="queue" />
        {entries && (
          <>
            <div className="overview-counts">
              <Link to="/queue?status=running">
                <strong>{running.length}</strong> Running
              </Link>
              <Link to="/queue?status=queued">
                <strong>
                  {entries.filter((entry) => entry.status === "queued").length}
                </strong>{" "}
                Queued
              </Link>
            </div>
            {entries.length === 0 ? (
              <p>No active builds in the queue snapshot.</p>
            ) : (
              runningPreview.length > 0 && (
                <ExecutionList rows={runningPreview} />
              )
            )}
          </>
        )}
      </section>
      <div className="overview-history">
        <section
          className="panel dashboard-panel"
          aria-labelledby="overview-failures"
        >
          <div className="dashboard-panel-header">
            <h3 id="overview-failures">Recent failures</h3>
            <Link to={`/builds?status=failed&limit=${HISTORY_LIMIT}&offset=0`}>
              Browse failed builds
            </Link>
          </div>
          <SnapshotNote query={history} source="recent builds" />
          {builds && (
            <>
              <p>
                {builds.length
                  ? `${failed.length} failed among ${builds.length} returned builds.`
                  : "No visible builds in this window."}{" "}
                Window: up to {HISTORY_LIMIT} recent builds.
              </p>
              {failures.length > 0 && <ExecutionList rows={failures} />}
            </>
          )}
        </section>
        <section
          className="panel dashboard-panel"
          aria-labelledby="overview-activity"
        >
          <div className="dashboard-panel-header">
            <h3 id="overview-activity">Other recent activity</h3>
            <Link to={historyURL}>Browse builds</Link>
          </div>
          {builds ? (
            activity.length ? (
              <ExecutionList rows={activity} />
            ) : (
              <p>No other executions in this window.</p>
            )
          ) : (
            <p>
              Recent build history is{" "}
              {history.error ? "unavailable" : "loading"}.
            </p>
          )}
        </section>
      </div>
      <section
        className="panel dashboard-panel"
        aria-labelledby="overview-workers"
      >
        <div className="dashboard-panel-header">
          <h3 id="overview-workers">Worker status</h3>
          <Link to="/workers">View workers</Link>
        </div>
        {reported && (
          <p>
            {visible.length
              ? `Visible workers: ${visible.filter((worker) => worker.status === "idle").length} idle · ${visible.filter((worker) => worker.status === "busy").length} busy · ${visible.filter((worker) => worker.status === "stale").length} stale.`
              : "No visible workers in this snapshot."}
            {hiddenCount > 0 && ` ${hiddenCount} older stale hidden.`}
          </p>
        )}
        <SnapshotNote query={workers} source="workers" />
      </section>
    </div>
  );
}
