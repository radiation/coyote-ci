import { useState } from "react";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { listBuilds, listJobsByProject } from "../api";
import {
  FAST_POLL_INTERVAL,
  isActiveBuild,
  SLOW_POLL_INTERVAL,
} from "../utils/build";
import { hydrateJobNames } from "../utils/jobNames";
import { BuildActivityList } from "./BuildActivityList";
import { RunJobAction } from "./RunJobAction";
import { StatusBadge } from "./StatusBadge";

export function ProjectJobsSection({
  id,
  compact = false,
}: {
  id: string;
  compact?: boolean;
}) {
  const [filter, setFilter] = useState("");
  const query = useQuery({
    queryKey: ["projectJobs", id],
    queryFn: () => listJobsByProject(id),
    refetchInterval: ({ state }) =>
      state.data?.some((job) => isActiveBuild(job.latest_build?.status))
        ? FAST_POLL_INTERVAL
        : SLOW_POLL_INTERVAL,
  });
  const jobs = (query.data ?? []).filter((job) => job.project_id === id);
  const visible = compact
    ? jobs.filter((job) => job.enabled).slice(0, 4)
    : jobs.filter((job) =>
        job.name.toLowerCase().includes(filter.trim().toLowerCase()),
      );
  return (
    <section className="detail-panel" aria-label="Project jobs">
      <div className="page-header-row">
        <h3>{compact ? "Jobs at a glance" : "Project jobs"}</h3>
        <Link to={`/jobs/new?project_id=${encodeURIComponent(id)}`}>
          Create Job
        </Link>
      </div>
      {!compact && (
        <label>
          Filter by job name{" "}
          <input
            type="search"
            value={filter}
            onChange={(event) => setFilter(event.target.value)}
          />
        </label>
      )}
      {query.isLoading ? (
        <p>Loading jobs…</p>
      ) : query.error ? (
        <p role="alert" className="error-text">
          Failed to load jobs: {String(query.error)}
        </p>
      ) : !jobs.length ? (
        <p>No jobs in this project yet.</p>
      ) : !visible.length ? (
        <p>
          {compact
            ? "No runnable jobs in this project."
            : "No jobs match your filter."}
        </p>
      ) : (
        <ul className="activity-list">
          {visible.map((job) => (
            <li key={job.id} className="activity-list-item project-job-row">
              <Link to={`/jobs/${job.id}`}>{job.name}</Link>
              {job.latest_build ? (
                <Link
                  aria-label={`Latest build for ${job.name}: ${job.latest_build.status}`}
                  to={`/builds/${job.latest_build.id}`}
                >
                  <StatusBadge status={job.latest_build.status} />
                </Link>
              ) : (
                <span className="subtle-text">Not run yet</span>
              )}
              {job.enabled ? (
                <RunJobAction jobID={job.id} jobName={job.name} />
              ) : (
                <span className="subtle-text">Disabled</span>
              )}
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

const pageSize = 20;

export function ProjectBuildsSection({ id }: { id: string }) {
  const { data: projectJobs } = useQuery({
    queryKey: ["projectJobs", id],
    queryFn: () => listJobsByProject(id),
  });
  const query = useInfiniteQuery({
    queryKey: ["projectBuildHistory", id],
    initialPageParam: 0,
    queryFn: ({ pageParam }) =>
      listBuilds({ project_id: id, limit: pageSize, offset: pageParam }),
    getNextPageParam: (last, _pages, offset) =>
      last.length === pageSize ? offset + pageSize : undefined,
    refetchInterval: ({ state }) =>
      state.data?.pages.some((page) =>
        page.some((build) => isActiveBuild(build.status)),
      )
        ? FAST_POLL_INTERVAL
        : SLOW_POLL_INTERVAL,
  });
  const builds = (query.data?.pages.flat() ?? []).filter(
    (build) => build.project_id === id,
  );
  const unique = new Map(builds.map((build) => [build.id, build]));
  const jobNames = new Map(
    (projectJobs ?? []).map((job) => [job.id, job.name]),
  );
  const namedBuilds = hydrateJobNames(Array.from(unique.values()), jobNames);
  return (
    <>
      <BuildActivityList
        title="Project builds"
        contextMode="project"
        items={namedBuilds.map((build) => ({
          kind: "build",
          build,
        }))}
        emptyMessage="No builds yet."
        loadingMessage={query.isLoading ? "Loading builds…" : undefined}
        error={!query.data && query.error}
        errorPrefix="Failed to load builds"
      />
      {query.isFetchNextPageError && (
        <p role="alert" className="error-text">
          Failed to load more builds: {String(query.error)}
        </p>
      )}
      {query.hasNextPage && (
        <button
          disabled={query.isFetchingNextPage}
          onClick={() => void query.fetchNextPage()}
        >
          {query.isFetchingNextPage ? "Loading more…" : "Load more"}
        </button>
      )}
    </>
  );
}
