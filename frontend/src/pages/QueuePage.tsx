import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useRef, useState } from "react";
import {
  Link,
  useLocation,
  useNavigate,
  useSearchParams,
} from "react-router-dom";
import {
  cancelBuild,
  listBuilds,
  listJobsByProject,
  listProjects,
  listQueue,
} from "../api";
import { PageHeader } from "../components/PageHeader";
import { BuildIdentityLink } from "../components/BuildIdentityLink";
import { StatusBadge } from "../components/StatusBadge";
import type { Build, QueueEntry } from "../types/build";
import {
  FAST_POLL_INTERVAL,
  SLOW_POLL_INTERVAL,
  isActiveBuild,
  isCancelableBuild,
  isTerminalBuild,
} from "../utils/build";
import { hydrateJobNames, missingJobNameProjectIDs } from "../utils/jobNames";
import { formatDuration, formatTime } from "../utils/time";

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
  entry?: QueueEntry;
  snapshotAt?: string;
  canceling?: boolean;
  onCancel?: (
    buildID: string,
    buildLabel: string,
    button: HTMLButtonElement,
  ) => void;
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
  entry,
  snapshotAt,
  canceling = false,
  onCancel,
}: QueueSectionRowProps) {
  const duration = formatDuration(startedAt, finishedAt);
  const buildLabel =
    buildNumber != null
      ? `Build #${buildNumber}`
      : `Build ${buildID.slice(0, 8)}`;

  return (
    <li
      className={`activity-list-item${status === "failed" ? " is-failed" : ""}`}
    >
      <div className="activity-list-main">
        <div className="activity-list-title-row">
          <BuildIdentityLink id={buildID} number={buildNumber} />
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
          {entry ? (
            <>
              <span>Priority {entry.priority ?? "—"}</span>
              <span>Queued {formatTime(queuedAt)}</span>
              {status === "running" ? (
                <>
                  <span>Started {formatTime(startedAt)}</span>
                  <span>Running {formatDuration(startedAt, snapshotAt)}</span>
                </>
              ) : (
                <span>Waiting {formatDuration(queuedAt, snapshotAt)}</span>
              )}
              <span>Worker {entry.worker_id || "Unassigned"}</span>
              <span>Lease expires {formatTime(entry.lease_expires_at)}</span>
            </>
          ) : null}
          {finishedAt ? <span>Finished {formatTime(finishedAt)}</span> : null}
          {finishedAt ? <span>Duration {duration}</span> : null}
        </div>
      </div>
      <div className="activity-list-meta subtle-text">
        <Link to={`/builds/${buildID}`}>Inspect</Link>
        {isCancelableBuild(status) && onCancel ? (
          <button
            className="inline-action-button danger-button"
            type="button"
            onClick={(event) =>
              onCancel(buildID, buildLabel, event.currentTarget)
            }
            aria-disabled={canceling}
          >
            Cancel
          </button>
        ) : null}
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
  activeStatus,
}: {
  title: string;
  items: React.JSX.Element[];
  loading: boolean;
  error: unknown;
  emptyMessage: string;
  activeStatus?: QueueEntry["status"];
}) {
  const [limit, setLimit] = useState(SECTION_LIMIT);
  const visible = activeStatus ? items.slice(0, limit) : items;
  return (
    <section className="panel dashboard-panel">
      <div className="dashboard-panel-header">
        <h3>{title}</h3>
      </div>
      {activeStatus && !loading && (!error || items.length > 0) ? (
        <p className="subtle-text">
          Showing {visible.length} of {items.length} {activeStatus} builds
        </p>
      ) : null}
      {!loading && !error && items.length === 0 ? (
        <div className="empty-state">
          <p className="empty">{emptyMessage}</p>
        </div>
      ) : null}
      {visible.length > 0 ? <ul className="activity-list">{visible}</ul> : null}
      {visible.length < items.length ? (
        <button
          className="secondary-button"
          type="button"
          onClick={() => setLimit(limit + SECTION_LIMIT)}
        >
          Show more {activeStatus} builds
        </button>
      ) : null}
    </section>
  );
}

export function QueuePage() {
  const [searchParams] = useSearchParams();
  const location = useLocation();
  const navigate = useNavigate();
  const activeHeading = useRef<HTMLHeadingElement>(null);
  const pending = useRef(false);
  const [feedback, setFeedback] = useState("");
  const queryClient = useQueryClient();
  const projectID = searchParams.get("project_id")?.trim() ?? "";
  const rawStatus = searchParams.get("status");
  const status =
    rawStatus === "queued" || rawStatus === "running" ? rawStatus : undefined;

  const {
    data: entries,
    isLoading: queueLoading,
    error: queueError,
    dataUpdatedAt: queueUpdatedAt,
  } = useQuery({
    queryKey: ["queue", projectID, status],
    queryFn: () =>
      listQueue({
        project_id: projectID || undefined,
        status,
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

  const cancelBuildMutation = useMutation({
    mutationFn: (buildID: string) => cancelBuild(buildID),
    onSuccess: async (_build, buildID) => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["queue"] }),
        queryClient.invalidateQueries({
          queryKey: ["queue-page", "recent-builds"],
        }),
        queryClient.invalidateQueries({ queryKey: ["build", buildID] }),
        queryClient.invalidateQueries({ queryKey: ["buildSteps", buildID] }),
      ]);
    },
  });

  function requestCancelBuild(
    buildID: string,
    label: string,
    button: HTMLButtonElement,
  ) {
    if (pending.current) return;
    const confirmed = window.confirm(`Cancel ${label}?`);
    if (!confirmed) {
      button.focus();
      return;
    }
    pending.current = true;
    setFeedback(`Canceling ${label}…`);
    cancelBuildMutation.mutate(buildID, {
      onSuccess: () => {
        setFeedback(`${label} canceled.`);
        if (
          document.activeElement === button ||
          document.activeElement === document.body
        )
          activeHeading.current?.focus();
      },
      onError: () => setFeedback(""),
      onSettled: () => {
        pending.current = false;
      },
    });
  }

  const projectIDsNeedingJobs = missingJobNameProjectIDs([
    ...(entries ?? []),
    ...(recentBuilds ?? []),
  ]);
  const { data: projectJobs = [] } = useQuery({
    queryKey: ["queue-page", "project-jobs", ...projectIDsNeedingJobs],
    queryFn: async () => {
      const responses = await Promise.all(
        projectIDsNeedingJobs.map((nextProjectID) =>
          listJobsByProject(nextProjectID),
        ),
      );
      return responses.flat();
    },
    enabled: projectIDsNeedingJobs.length > 0,
  });

  function updateFilters(key: "project_id" | "status", value: string) {
    const nextParams = new URLSearchParams(searchParams);
    if (value) nextParams.set(key, value);
    else nextParams.delete(key);
    navigate({
      pathname: location.pathname,
      search: nextParams.toString(),
      hash: location.hash,
    });
  }

  const projectJobNames = new Map(projectJobs.map((job) => [job.id, job.name]));
  const normalizedEntries = hydrateJobNames(entries ?? [], projectJobNames);
  const normalizedRecentBuilds = hydrateJobNames(
    recentBuilds ?? [],
    projectJobNames,
  );

  const runningEntries = normalizedEntries.filter(
    (entry) => entry.status === "running",
  );
  const queuedEntries = normalizedEntries.filter(
    (entry) => entry.status === "queued",
  );
  const terminalBuilds = sortByNewest(
    normalizedRecentBuilds.filter((build) => isTerminalBuild(build.status)),
  );
  const failedBuilds = terminalBuilds
    .filter((build) => build.status === "failed")
    .slice(0, SECTION_LIMIT);
  const recentTerminalBuilds = terminalBuilds
    .filter((build) => build.status !== "failed")
    .slice(0, SECTION_LIMIT);
  const snapshotAt = new Date(queueUpdatedAt).toISOString();
  const activeRow = (entry: QueueEntry) => (
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
      queuedAt={entry.queued_at}
      snapshotAt={snapshotAt}
      entry={entry}
      startedAt={entry.started_at}
      canceling={cancelBuildMutation.isPending}
      onCancel={requestCancelBuild}
    />
  );

  const terminalRow = (build: Build) => (
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
      startedAt={build.started_at}
      finishedAt={build.finished_at}
    />
  );

  return (
    <div className="page-content page-queue">
      <PageHeader
        title="Queue"
        description="Active and recent build execution across accessible projects."
      />

      <div role="status">{feedback}</div>
      {cancelBuildMutation.error ? (
        <p role="alert" className="error-text">
          Failed to cancel build: {String(cancelBuildMutation.error)}
        </p>
      ) : null}

      <section className="artifact-filters-panel" aria-label="Queue filters">
        <div className="artifact-filters">
          <label className="artifact-filter-field artifact-filter-select">
            <span>Project</span>
            <select
              value={projectID}
              onChange={(event) =>
                updateFilters("project_id", event.target.value)
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
            <span>Active work status</span>
            <select
              value={status ?? ""}
              onChange={(event) => updateFilters("status", event.target.value)}
            >
              <option value="">All active</option>
              <option value="queued">Queued</option>
              <option value="running">Running</option>
            </select>
          </label>
        </div>
      </section>

      <h2 ref={activeHeading} tabIndex={-1}>
        Active work
      </h2>
      <FeedState
        name="active work"
        loading={queueLoading}
        error={queueError}
        updatedAt={queueUpdatedAt}
      />
      <div
        className="queue-sections-grid"
        key={JSON.stringify([projectID, status])}
      >
        {status !== "running" && (
          <QueueSection
            title="Queued builds"
            activeStatus="queued"
            items={queuedEntries.map(activeRow)}
            loading={queueLoading}
            error={queueError}
            emptyMessage="Nothing is queued."
          />
        )}
        {status !== "queued" && (
          <QueueSection
            title="Running builds"
            activeStatus="running"
            items={runningEntries.map(activeRow)}
            loading={queueLoading}
            error={queueError}
            emptyMessage="No running builds."
          />
        )}
      </div>
      <h2>Recent outcomes</h2>
      <div>
        From up to {RECENT_BUILD_FETCH_LIMIT} recent builds ·{" "}
        <Link
          to={{
            pathname: "/builds",
            search: projectID
              ? new URLSearchParams({ project_id: projectID }).toString()
              : "",
          }}
        >
          View build history
        </Link>
      </div>
      <FeedState
        name="recent outcomes"
        loading={recentBuildsLoading}
        error={recentBuildsError}
        updatedAt={recentBuildsUpdatedAt}
      />
      <div className="queue-sections-grid">
        <QueueSection
          title="Recent failed builds"
          items={failedBuilds.map(terminalRow)}
          loading={recentBuildsLoading}
          error={recentBuildsError}
          emptyMessage="No recent failures."
        />
        <QueueSection
          title="Other recent completed builds"
          items={recentTerminalBuilds.map(terminalRow)}
          loading={recentBuildsLoading}
          error={recentBuildsError}
          emptyMessage="No recent terminal builds."
        />
      </div>
    </div>
  );
}

function FeedState({
  name,
  loading,
  error,
  updatedAt,
}: {
  name: string;
  loading: boolean;
  error: unknown;
  updatedAt: number;
}) {
  return (
    <div className="subtle-text">
      {loading && <span role="status">Loading {name}…</span>}
      {error ? (
        <span role="alert">
          Unable to load {name}.
          {updatedAt ? " Showing last known results." : ""}{" "}
        </span>
      ) : null}
      {updatedAt > 0 && (
        <>Last updated {formatTime(new Date(updatedAt).toISOString())}</>
      )}
    </div>
  );
}
