import { useQuery } from "@tanstack/react-query";
import { Link, useParams } from "react-router-dom";
import { getProject, listJobsByProject } from "../api";
import { formatTime } from "../utils/time";

export function ProjectDetailPage() {
  const { id } = useParams<{ id: string }>();

  const {
    data: project,
    isLoading,
    error,
  } = useQuery({
    queryKey: ["project", id],
    queryFn: () => getProject(id!),
    enabled: Boolean(id),
  });

  const {
    data: jobs,
    isLoading: jobsLoading,
    error: jobsError,
  } = useQuery({
    queryKey: ["projectJobs", id],
    queryFn: () => listJobsByProject(id!),
    enabled: Boolean(id),
  });

  if (isLoading) {
    return <p>Loading project…</p>;
  }

  if (error) {
    return (
      <p className="error-text">Failed to load project: {String(error)}</p>
    );
  }

  if (!project) {
    return <p className="error-text">Project not found.</p>;
  }

  return (
    <>
      <Link to="/projects">← Back to projects</Link>
      <div className="page-header-row">
        <div>
          <h2>{project.name}</h2>
          <p className="subtle-text">Slug: {project.slug}</p>
        </div>
        <Link className="action-link" to="/jobs/new">
          Create Job
        </Link>
      </div>

      <div className="detail-grid">
        <div>
          <strong>ID</strong>
          <span>{project.id}</span>
        </div>
        <div>
          <strong>Description</strong>
          <span>{project.description || "—"}</span>
        </div>
        <div>
          <strong>Created</strong>
          <span>{formatTime(project.created_at)}</span>
        </div>
        <div>
          <strong>Updated</strong>
          <span>{formatTime(project.updated_at)}</span>
        </div>
      </div>

      <h3>Jobs</h3>
      {jobsLoading && <p>Loading jobs…</p>}
      {jobsError && (
        <p className="error-text">Failed to load jobs: {String(jobsError)}</p>
      )}
      {!jobsLoading && !jobsError && jobs && jobs.length === 0 && (
        <p className="subtle-text">No jobs in this project yet.</p>
      )}
      {jobs && jobs.length > 0 && (
        <table className="table">
          <thead>
            <tr>
              <th>Name</th>
              <th>Repository</th>
              <th>Default Ref</th>
              <th>Updated</th>
            </tr>
          </thead>
          <tbody>
            {jobs.map((job) => (
              <tr key={job.id}>
                <td>
                  <Link to={`/jobs/${job.id}`}>{job.name}</Link>
                </td>
                <td>{job.repository_url}</td>
                <td>{job.default_ref}</td>
                <td>{formatTime(job.updated_at)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </>
  );
}
