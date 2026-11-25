import { useState } from "react";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { listBuilds, listJobsByProject } from "../api";
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
  });
  const jobs = (query.data ?? []).filter((job) => job.project_id === id);
  const visible = compact
    ? jobs.slice(0, 4)
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
        <p>No jobs match your filter.</p>
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
              <RunJobAction jobID={job.id} jobName={job.name} />
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

const pageSize = 20;

export function ProjectBuildsSection({ id }: { id: string }) {
  const query = useInfiniteQuery({
    queryKey: ["projectBuildHistory", id],
    initialPageParam: 0,
    queryFn: ({ pageParam }) =>
      listBuilds({ project_id: id, limit: pageSize, offset: pageParam }),
    getNextPageParam: (last, _pages, offset) =>
      last.length === pageSize ? offset + pageSize : undefined,
  });
  const builds = (query.data?.pages.flat() ?? []).filter(
    (build) => build.project_id === id,
  );
  const unique = new Map(builds.map((build) => [build.id, build]));
  return (
    <>
      <BuildActivityList
        title="Project builds"
        contextMode="project"
        items={Array.from(unique.values(), (build) => ({
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
