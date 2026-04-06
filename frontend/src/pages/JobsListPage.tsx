import { useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "react-router-dom";
import { listJobs, runJob } from "../api";
import { formatTime } from "../utils/time";

export function JobsListPage() {
  const navigate = useNavigate();
  const [runError, setRunError] = useState<string | null>(null);

  const {
    data: jobs,
    isLoading,
    error,
    dataUpdatedAt,
  } = useQuery({
    queryKey: ["jobs"],
    queryFn: listJobs,
  });

  const runNowMutation = useMutation({
    mutationFn: (jobID: string) => runJob(jobID),
    onMutate: () => {
      setRunError(null);
    },
    onSuccess: (build) => {
      if (build.id) {
        navigate(`/builds/${build.id}`);
        return;
      }
      setRunError("Run succeeded but no build id was returned.");
    },
    onError: (mutationError) => {
      setRunError(`Failed to run job: ${String(mutationError)}`);
    },
  });

  return (
    <>
      <div className="page-header-row">
        <div>
          <h2>Jobs</h2>
          <p className="subtle-text">
            Last updated:{" "}
            {dataUpdatedAt > 0
              ? formatTime(new Date(dataUpdatedAt).toISOString())
              : "—"}
          </p>
        </div>
        <Link className="action-link" to="/jobs/new">
          Create Job
        </Link>
      </div>

      {runError && <p className="error-text">{runError}</p>}
      {isLoading && <p>Loading jobs…</p>}
      {error && (
        <p className="error-text">Failed to load jobs: {String(error)}</p>
      )}

      {!isLoading && !error && jobs && jobs.length === 0 && (
        <div className="empty-state">
          <p className="empty">No jobs yet.</p>
          <p className="subtle-text">
            Create a job to store reusable pipeline configuration and run it on
            demand.
          </p>
        </div>
      )}

      {jobs && jobs.length > 0 && (
        <table className="table">
          <thead>
            <tr>
              <th>Name</th>
              <th>Repository</th>
              <th>Default Ref</th>
              <th>Enabled</th>
              <th>Push Trigger</th>
              <th>Updated</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            {jobs.map((job) => (
              <tr key={job.id}>
                <td>{job.name}</td>
                <td>{job.repository_url}</td>
                <td>{job.default_ref}</td>
                <td>{job.enabled ? "Enabled" : "Disabled"}</td>
                <td>
                  {job.push_enabled
                    ? job.push_branch
                      ? `On ${job.push_branch}`
                      : "Any branch"
                    : "Off"}
                </td>
                <td>{formatTime(job.updated_at)}</td>
                <td>
                  <div className="table-actions">
                    <Link to={`/jobs/${job.id}`}>Open</Link>
                    <button
                      type="button"
                      className="table-action-button"
                      onClick={() => runNowMutation.mutate(job.id)}
                      disabled={runNowMutation.isPending}
                    >
                      {runNowMutation.isPending &&
                      runNowMutation.variables === job.id
                        ? "Running…"
                        : "Run Now"}
                    </button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </>
  );
}
