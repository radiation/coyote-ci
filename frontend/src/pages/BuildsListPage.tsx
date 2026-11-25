import { useQuery } from "@tanstack/react-query";
import { Link, useSearchParams } from "react-router-dom";
import { listBuilds, listProjects } from "../api";
import { StatusBadge } from "../components/StatusBadge";
import { PageHeader } from "../components/PageHeader";
import { BuildIdentityLink } from "../components/BuildIdentityLink";
import {
  historyParams,
  historyStatuses,
  readBuildHistory,
} from "../utils/buildHistory";
import type { Build } from "../types/build";
import {
  FAST_POLL_INTERVAL,
  SLOW_POLL_INTERVAL,
  isActiveBuild,
} from "../utils/build";
import { formatDuration, formatTime } from "../utils/time";

function shortSHA(value: string | null | undefined): string {
  const trimmed = (value ?? "").trim();
  if (!trimmed) return "";
  return trimmed.slice(0, 7);
}

function triggerKind(build: Build): string {
  return (build.trigger_kind ?? "manual").trim() || "manual";
}

function compactTriggerMetadata(build: Build): string {
  const parts: string[] = [];
  const provider = (build.scm_provider ?? "").trim();
  const ref = (build.trigger_ref ?? "").trim();
  const sha = shortSHA(build.trigger_commit_sha ?? build.source_commit_sha);
  const actor = (build.actor ?? "").trim();

  if (provider) parts.push(provider);
  if (ref) parts.push(ref);
  if (sha) parts.push(sha);
  if (actor) parts.push(actor);
  return parts.join(" • ");
}

export function BuildsListPage() {
  const [searchParams, setSearchParams] = useSearchParams();
  const history = readBuildHistory(searchParams);
  const { project: projectID, limit, offset, status } = history;
  const {
    data: builds,
    isLoading,
    error,
    dataUpdatedAt,
  } = useQuery({
    queryKey: ["builds", projectID, limit, offset],
    queryFn: () =>
      listBuilds({ project_id: projectID || undefined, limit, offset }),
    refetchInterval: (query) => {
      const nextBuilds = query.state.data as Build[] | undefined;
      if (!nextBuilds || nextBuilds.length === 0) {
        return SLOW_POLL_INTERVAL;
      }

      return nextBuilds.some((b) => isActiveBuild(b.status))
        ? FAST_POLL_INTERVAL
        : SLOW_POLL_INTERVAL;
    },
  });
  const { data: projects = [] } = useQuery({
    queryKey: ["projects"],
    queryFn: () => listProjects(),
  });

  const visible =
    builds?.filter((build) => !status || build.status === status) ?? [];
  const detailSearch = new URLSearchParams({
    history: historyParams(history).toString(),
  }).toString();
  const pageLink = (nextOffset: number) =>
    `?${historyParams({ ...history, offset: nextOffset })}`;

  return (
    <div className="page-content build-history">
      <PageHeader
        title="Builds"
        description={
          <>
            Builds are created by running a <Link to="/jobs">job</Link>. Last
            updated:{" "}
            {dataUpdatedAt > 0
              ? formatTime(new Date(dataUpdatedAt).toISOString())
              : "—"}
          </>
        }
      />
      <div className="artifact-filters">
        <label className="artifact-filter-field artifact-filter-select">
          <span>Project</span>
          <select
            value={projectID}
            onChange={(event) =>
              setSearchParams(
                historyParams({
                  ...history,
                  project: event.target.value,
                  offset: 0,
                }),
              )
            }
          >
            <option value="">All projects</option>
            {projects.map((project) => (
              <option key={project.id} value={project.id}>
                {project.name} ({project.slug})
              </option>
            ))}
          </select>
        </label>

        <label className="artifact-filter-field artifact-filter-select">
          <span>Status on this page</span>
          <select
            value={status}
            onChange={(event) =>
              setSearchParams(
                historyParams({ ...history, status: event.target.value }),
              )
            }
          >
            <option value="">All statuses</option>
            {historyStatuses.map((value) => (
              <option key={value} value={value}>
                {value[0].toUpperCase() + value.slice(1)}
              </option>
            ))}
          </select>
        </label>
      </div>
      <nav className="activity-list-title-row" aria-label="Build history pages">
        {offset > 0 ? (
          <Link to={pageLink(Math.max(0, offset - limit))}>Newer</Link>
        ) : (
          <span aria-disabled="true">Newer</span>
        )}
        <span>
          Offset {offset} · Page size {limit}
        </span>
        {Number.isSafeInteger(offset + limit) && (
          <Link to={pageLink(offset + limit)}>Older</Link>
        )}
      </nav>
      {isLoading && <p role="status">Loading builds…</p>}
      {error && (
        <p role="alert" className="error-text">
          Failed to load builds: {String(error)}
        </p>
      )}
      {!isLoading && !error && visible.length === 0 && (
        <p className="empty" role="status">
          {builds?.length
            ? `No ${status} builds among the ${builds.length} results on this page.`
            : "No visible builds on this page."}{" "}
          View older builds to continue searching.
        </p>
      )}

      {visible.length > 0 && (
        <ul className="activity-list" aria-label="Build history">
          {visible.map((b) => (
            <li key={b.id} className="activity-list-item">
              <div className="activity-list-main">
                <div className="activity-list-title-row">
                  <BuildIdentityLink
                    id={b.id}
                    number={b.build_number}
                    search={detailSearch}
                  />
                  <StatusBadge status={b.status} />
                </div>
                <div className="subtle-text">
                  <Link to={`/projects/${b.project_id}`}>
                    {b.project_name?.trim() ||
                      b.project_slug?.trim() ||
                      b.project_id}
                  </Link>
                  {" · "}
                  {b.job_id ? (
                    <Link to={`/jobs/${b.job_id}`}>
                      {b.job_name?.trim() || b.job_id}
                    </Link>
                  ) : (
                    b.job_name?.trim() || "No linked job"
                  )}
                </div>
                <div className="activity-list-detail-grid subtle-text">
                  <span>Created {formatTime(b.created_at)}</span>
                  {b.started_at && (
                    <span>Started {formatTime(b.started_at)}</span>
                  )}
                  {b.finished_at && (
                    <>
                      <span>Finished {formatTime(b.finished_at)}</span>
                      <span>
                        Duration {formatDuration(b.started_at, b.finished_at)}
                      </span>
                    </>
                  )}
                </div>
                <details>
                  <summary>Execution details</summary>
                  <div>
                    Priority {b.priority} · Step index {b.current_step_index} ·
                    Queued {formatTime(b.queued_at)}
                  </div>
                  <div className="trigger-cell">
                    <span className={`trigger-badge trigger-${triggerKind(b)}`}>
                      {triggerKind(b)}
                    </span>
                    <span className="trigger-meta">
                      {compactTriggerMetadata(b) || "—"}
                    </span>
                  </div>
                  {b.error_message && (
                    <p className="error-text">{b.error_message}</p>
                  )}
                </details>
              </div>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
