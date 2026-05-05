import { useQuery } from "@tanstack/react-query";
import { Link, useSearchParams } from "react-router-dom";
import { listProjects, listQueue } from "../api";
import { StatusBadge } from "../components/StatusBadge";
import type { QueueEntry } from "../types/build";
import {
  FAST_POLL_INTERVAL,
  SLOW_POLL_INTERVAL,
  isActiveBuild,
} from "../utils/build";
import { formatTime } from "../utils/time";

function queueMeta(entry: QueueEntry): string {
  const parts: string[] = [];
  const ref = (entry.trigger_ref ?? "").trim();
  const sha = (
    entry.trigger_commit_sha ??
    entry.source_commit_sha ??
    ""
  ).trim();

  if (ref) {
    parts.push(ref);
  }
  if (sha) {
    parts.push(sha.slice(0, 7));
  }

  return parts.join(" • ");
}

export function QueuePage() {
  const [searchParams, setSearchParams] = useSearchParams();
  const projectID = searchParams.get("project_id")?.trim() ?? "";
  const status = searchParams.get("status")?.trim() ?? "";
  const normalizedStatus =
    status === "queued" || status === "running" ? status : "";

  const {
    data: entries,
    isLoading,
    error,
    dataUpdatedAt,
  } = useQuery({
    queryKey: ["queue", projectID, normalizedStatus],
    queryFn: () =>
      listQueue({
        project_id: projectID || undefined,
        status: normalizedStatus || undefined,
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

  const { data: projects = [] } = useQuery({
    queryKey: ["projects"],
    queryFn: () => listProjects(),
  });

  function updateFilters(next: { projectID?: string; status?: string }) {
    const nextParams = new URLSearchParams(searchParams);
    const nextProjectID = next.projectID ?? projectID;
    const nextStatus = next.status ?? normalizedStatus;

    if (nextProjectID) {
      nextParams.set("project_id", nextProjectID);
    } else {
      nextParams.delete("project_id");
    }
    if (nextStatus) {
      nextParams.set("status", nextStatus);
    } else {
      nextParams.delete("status");
    }

    setSearchParams(nextParams, { replace: true });
  }

  return (
    <>
      <h2>Queue</h2>
      <p className="subtle-text">
        Shows queued and running builds in dispatch order. Last updated:{" "}
        {dataUpdatedAt > 0
          ? formatTime(new Date(dataUpdatedAt).toISOString())
          : "—"}
      </p>

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

        <label className="artifact-filter-field artifact-filter-select">
          <span>Status</span>
          <select
            value={normalizedStatus}
            onChange={(event) => updateFilters({ status: event.target.value })}
          >
            <option value="">Queued and running</option>
            <option value="queued">Queued</option>
            <option value="running">Running</option>
          </select>
        </label>
      </div>

      {isLoading && <p>Loading queue…</p>}
      {error && (
        <p className="error-text">Failed to load queue: {String(error)}</p>
      )}
      {!isLoading && !error && (!entries || entries.length === 0) && (
        <p className="empty">No queued or running builds.</p>
      )}

      {entries && entries.length > 0 && (
        <table className="table">
          <thead>
            <tr>
              <th>Build</th>
              <th>Project</th>
              <th>Job</th>
              <th>Priority</th>
              <th>Status</th>
              <th>Queued</th>
              <th>Started</th>
              <th>Worker</th>
              <th>Lease</th>
              <th>Source</th>
            </tr>
          </thead>
          <tbody>
            {entries.map((entry) => (
              <tr key={entry.build_id}>
                <td>
                  <Link to={`/builds/${entry.build_id}`}>
                    #{entry.build_number}
                  </Link>
                </td>
                <td>
                  <Link to={`/projects/${entry.project_id}`}>
                    {entry.project_name?.trim() ||
                      entry.project_slug?.trim() ||
                      entry.project_id}
                  </Link>
                </td>
                <td>{entry.job_name?.trim() || entry.job_id || "Manual"}</td>
                <td>{entry.priority}</td>
                <td>
                  <StatusBadge status={entry.status} />
                </td>
                <td>{formatTime(entry.queued_at ?? entry.created_at)}</td>
                <td>{formatTime(entry.started_at)}</td>
                <td>{entry.worker_id ?? "—"}</td>
                <td>{formatTime(entry.lease_expires_at)}</td>
                <td>{queueMeta(entry) || entry.repository_url || "—"}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </>
  );
}
