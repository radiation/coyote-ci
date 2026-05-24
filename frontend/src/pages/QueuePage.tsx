import { useQuery } from "@tanstack/react-query";
import { Link, useSearchParams } from "react-router-dom";
import { listBuilds, listProjects, listQueue } from "../api";
import { PageHeader } from "../components/PageHeader";
import { StatusBadge } from "../components/StatusBadge";
import type { Build, QueueEntry } from "../types/build";
import {
  FAST_POLL_INTERVAL,
  SLOW_POLL_INTERVAL,
  isActiveBuild,
} from "../utils/build";
import { formatTime } from "../utils/time";

const RECENT_BUILD_FETCH_LIMIT = 24;
const SECTION_LIMIT = 8;

type QueueSectionRowProps = {
  buildID: string;
  buildNumber?: number | null;
  status: Build["status"];
  projectID: string;
  projectName?: string | null;
  projectSlug?: string | null;
  jobID?: string | null;
  jobName?: string | null;
  queuedAt?: string | null;
  startedAt?: string | null;
  finishedAt?: string | null;
};

function projectLabel(
  projectName: string | null | undefined,
  projectSlug: string | null | undefined,
  projectID: string,
): string {
  return projectName?.trim() || projectSlug?.trim() || projectID;
}

function jobLabel(
  jobName: string | null | undefined,
  jobID: string | null | undefined,
): string {
  return jobName?.trim() || jobID?.trim() || "Manual";
}

function formatDuration(
  startISO: string | null | undefined,
  endISO: string | null | undefined,
): string | null {
  if (!startISO || !endISO) {
    return null;
  }

  const start = Date.parse(startISO);
  const end = Date.parse(endISO);
  if (Number.isNaN(start) || Number.isNaN(end) || end < start) {
    return null;
  }

  const totalSeconds = Math.floor((end - start) / 1000);
  const hours = Math.floor(totalSeconds / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = totalSeconds % 60;

  if (hours > 0) {
    return `${hours}h ${minutes}m`;
  }
  if (minutes > 0) {
    return `${minutes}m ${seconds}s`;
  }
  return `${seconds}s`;
}

function sortByNewest(builds: Build[]): Build[] {
  return [...builds].sort((left, right) => {
    const leftTime = Date.parse(
      left.finished_at ?? left.started_at ?? left.queued_at ?? left.created_at,
    );
    const rightTime = Date.parse(
      right.finished_at ??
        right.started_at ??
        right.queued_at ??
        right.created_at,
    );

    return rightTime - leftTime;
  });
}

function QueueSectionRow({
  buildID,
  buildNumber,
  status,
  projectID,
  projectName,
  projectSlug,
  jobID,
  jobName,
  queuedAt,
  startedAt,
  finishedAt,
}: QueueSectionRowProps) {
  const duration = formatDuration(startedAt, finishedAt);
  const buildLabel = buildNumber
    ? `Build #${buildNumber}`
    : `Build ${buildID.slice(0, 8)}`;

  return (
    <li
      className={`activity-list-item${status === "failed" ? " is-failed" : ""}`}
    >
      <div className="activity-list-main">
        <div className="activity-list-title-row">
          <Link to={`/builds/${buildID}`}>{buildLabel}</Link>
          <StatusBadge status={status} />
        </div>
        <p className="subtle-text activity-list-context">
          <Link to={`/projects/${projectID}`}>
            {projectLabel(projectName, projectSlug, projectID)}
          </Link>
          {jobID ? (
            <>
              {" · "}
              <Link to={`/jobs/${jobID}`}>{jobLabel(jobName, jobID)}</Link>
            </>
          ) : (
            ` · ${jobLabel(jobName, jobID)}`
          )}
        </p>
        <div className="activity-list-detail-grid subtle-text">
          <span>Queued {formatTime(queuedAt)}</span>
          {startedAt ? <span>Started {formatTime(startedAt)}</span> : null}
          {finishedAt ? <span>Finished {formatTime(finishedAt)}</span> : null}
          {duration ? <span>Duration {duration}</span> : null}
        </div>
      </div>
      <div className="activity-list-meta subtle-text">
        <Link to={`/builds/${buildID}`}>Inspect</Link>
      </div>
    </li>
  );
}

function QueueSection({
  title,
  items,
  loading,
  error,
  emptyMessage,
}: {
  title: string;
  items: React.JSX.Element[];
  loading: boolean;
  error: unknown;
  emptyMessage: string;
}) {
  return (
    <section className="panel dashboard-panel">
      <div className="dashboard-panel-header">
        <h3>{title}</h3>
      </div>
      {loading ? <p>Loading…</p> : null}
      {error ? <p className="error-text">Unable to load.</p> : null}
      {!loading && !error && items.length === 0 ? (
        <div className="empty-state">
          <p className="empty">{emptyMessage}</p>
        </div>
      ) : null}
      {!loading && !error && items.length > 0 ? (
        <ul className="activity-list">{items}</ul>
      ) : null}
    </section>
  );
}

export function QueuePage() {
  const [searchParams, setSearchParams] = useSearchParams();
  const projectID = searchParams.get("project_id")?.trim() ?? "";

  const {
    data: entries,
    isLoading: queueLoading,
    error: queueError,
    dataUpdatedAt: queueUpdatedAt,
  } = useQuery({
    queryKey: ["queue", projectID],
    queryFn: () =>
      listQueue({
        project_id: projectID || undefined,
      }),
    refetchInterval: (query) => {
      const nextEntries = query.state.data as QueueEntry[] | undefined;
      if (!nextEntries || nextEntries.length === 0) {
        return SLOW_POLL_INTERVAL;
      }

      return nextEntries.some((entry) => isActiveBuild(entry.status))
        ? FAST_POLL_INTERVAL
        : SLOW_POLL_INTERVAL;
    },
  });

  const {
    data: recentBuilds,
    isLoading: recentBuildsLoading,
    error: recentBuildsError,
    dataUpdatedAt: recentBuildsUpdatedAt,
  } = useQuery({
    queryKey: ["queue-page", "recent-builds", projectID],
    queryFn: () =>
      listBuilds({
        project_id: projectID || undefined,
        limit: RECENT_BUILD_FETCH_LIMIT,
      }),
    refetchInterval: (query) => {
      const nextBuilds = query.state.data as Build[] | undefined;
      if (!nextBuilds || nextBuilds.length === 0) {
        return SLOW_POLL_INTERVAL;
      }

      return nextBuilds.some((build) => isActiveBuild(build.status))
        ? FAST_POLL_INTERVAL
        : SLOW_POLL_INTERVAL;
    },
  });

  const { data: projects = [] } = useQuery({
    queryKey: ["projects"],
    queryFn: () => listProjects(),
  });

  function updateFilters(next: { projectID?: string }) {
    const nextParams = new URLSearchParams(searchParams);
    const nextProjectID = next.projectID ?? projectID;

    if (nextProjectID) {
      nextParams.set("project_id", nextProjectID);
    } else {
      nextParams.delete("project_id");
    }
    nextParams.delete("status");

    setSearchParams(nextParams, { replace: true });
  }

  const runningEntries = (entries ?? []).filter(
    (entry) => entry.status === "running",
  );
  const queuedEntries = (entries ?? []).filter(
    (entry) => entry.status === "queued",
  );
  const terminalBuilds = sortByNewest(
    (recentBuilds ?? []).filter(
      (build) => build.status === "success" || build.status === "failed",
    ),
  );
  const failedBuilds = terminalBuilds
    .filter((build) => build.status === "failed")
    .slice(0, SECTION_LIMIT);
  const completedBuilds = terminalBuilds
    .filter((build) => build.status === "success")
    .slice(0, SECTION_LIMIT);
  const lastUpdatedAt = Math.max(queueUpdatedAt, recentBuildsUpdatedAt);

  const runningItems = runningEntries
    .slice(0, SECTION_LIMIT)
    .map((entry) => (
      <QueueSectionRow
        key={entry.build_id}
        buildID={entry.build_id}
        buildNumber={entry.build_number}
        status={entry.status}
        projectID={entry.project_id}
        projectName={entry.project_name}
        projectSlug={entry.project_slug}
        jobID={entry.job_id}
        jobName={entry.job_name}
        queuedAt={entry.queued_at ?? entry.created_at}
        startedAt={entry.started_at}
      />
    ));

  const queuedItems = queuedEntries
    .slice(0, SECTION_LIMIT)
    .map((entry) => (
      <QueueSectionRow
        key={entry.build_id}
        buildID={entry.build_id}
        buildNumber={entry.build_number}
        status={entry.status}
        projectID={entry.project_id}
        projectName={entry.project_name}
        projectSlug={entry.project_slug}
        jobID={entry.job_id}
        jobName={entry.job_name}
        queuedAt={entry.queued_at ?? entry.created_at}
        startedAt={entry.started_at}
      />
    ));

  const failedItems = failedBuilds.map((build) => (
    <QueueSectionRow
      key={build.id}
      buildID={build.id}
      buildNumber={build.build_number}
      status={build.status}
      projectID={build.project_id}
      projectName={build.project_name}
      projectSlug={build.project_slug}
      jobID={build.job_id}
      jobName={build.job_name}
      queuedAt={build.queued_at ?? build.created_at}
      startedAt={build.started_at}
      finishedAt={build.finished_at}
    />
  ));

  const completedItems = completedBuilds.map((build) => (
    <QueueSectionRow
      key={build.id}
      buildID={build.id}
      buildNumber={build.build_number}
      status={build.status}
      projectID={build.project_id}
      projectName={build.project_name}
      projectSlug={build.project_slug}
      jobID={build.job_id}
      jobName={build.job_name}
      queuedAt={build.queued_at ?? build.created_at}
      startedAt={build.started_at}
      finishedAt={build.finished_at}
    />
  ));

  return (
    <div className="page-content page-queue">
      <PageHeader
        title="Queue"
        description={
          <>
            Active and recent build execution across accessible projects. Last
            updated{" "}
            {lastUpdatedAt > 0
              ? formatTime(new Date(lastUpdatedAt).toISOString())
              : "—"}
            .
          </>
        }
      />

      <section className="artifact-filters-panel" aria-label="Queue filters">
        <div className="artifact-filters">
          <label className="artifact-filter-field artifact-filter-select">
            <span>Project</span>
            <select
              value={projectID}
              onChange={(event) =>
                updateFilters({ projectID: event.target.value })
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
        </div>
      </section>

      <div className="queue-sections-grid">
        <QueueSection
          title="Running"
          items={runningItems}
          loading={queueLoading}
          error={queueError}
          emptyMessage="No running builds."
        />
        <QueueSection
          title="Queued"
          items={queuedItems}
          loading={queueLoading}
          error={queueError}
          emptyMessage="Nothing is queued."
        />
        <QueueSection
          title="Recent failed"
          items={failedItems}
          loading={recentBuildsLoading}
          error={recentBuildsError}
          emptyMessage="No recent failures."
        />
        <QueueSection
          title="Recent completed"
          items={completedItems}
          loading={recentBuildsLoading}
          error={recentBuildsError}
          emptyMessage="No recent completed builds."
        />
      </div>
    </div>
  );
}
