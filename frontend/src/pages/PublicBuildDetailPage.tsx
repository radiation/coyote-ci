import { useQuery } from "@tanstack/react-query";
import { Link, Navigate, useParams } from "react-router-dom";
import { getPublicBuild, getPublicProject, isAPIErrorStatus } from "../api";
import { useAuth } from "../auth-context";
import { StatusBadge } from "../components/StatusBadge";
import type { PublicBuild } from "../types/public";
import {
  FAST_POLL_INTERVAL,
  isActiveBuild,
  SLOW_POLL_INTERVAL,
} from "../utils/build";
import { formatTime } from "../utils/time";

export function PublicBuildDetailPage() {
  const { authStatus } = useAuth();

  return authStatus === "authenticated" ? (
    <AuthenticatedPublicBuildRoute />
  ) : (
    <AnonymousPublicBuildDetailPage />
  );
}

function AuthenticatedPublicBuildRoute() {
  const { slug, buildID } = useParams<{ slug: string; buildID: string }>();
  const {
    data: publicProject,
    isLoading,
    error,
  } = useQuery({
    queryKey: ["publicProject", slug],
    queryFn: () => getPublicProject(slug!),
    enabled: Boolean(slug),
    retry: false,
  });

  if (isLoading) return <p>Loading build…</p>;
  if (publicProject && buildID) {
    return <Navigate to={`/builds/${buildID}`} replace />;
  }
  if (error && isAPIErrorStatus(error, 404)) {
    return <p className="error-text">Build not found.</p>;
  }
  if (error) {
    return <p className="error-text">Failed to load build: {String(error)}</p>;
  }
  return <p className="error-text">Build not found.</p>;
}

function AnonymousPublicBuildDetailPage() {
  const { slug, buildID } = useParams<{ slug: string; buildID: string }>();
  const {
    data: build,
    isLoading,
    error,
  } = useQuery({
    queryKey: ["publicBuild", slug, buildID],
    queryFn: () => getPublicBuild(slug!, buildID!),
    enabled: Boolean(slug && buildID),
    refetchInterval: (query) => {
      const nextBuild = query.state.data as PublicBuild | undefined;
      return isActiveBuild(nextBuild?.status)
        ? FAST_POLL_INTERVAL
        : SLOW_POLL_INTERVAL;
    },
  });

  if (isLoading) return <p>Loading build…</p>;
  if (error && isAPIErrorStatus(error, 404)) {
    return <p className="error-text">Build not found.</p>;
  }
  if (error) {
    return <p className="error-text">Failed to load build: {String(error)}</p>;
  }
  if (!build) return <p className="error-text">Build not found.</p>;

  return (
    <div className="page-content page-build-detail">
      <Link to={`/projects/${slug}`}>← Back to project</Link>
      <div className="page-header-row">
        <div className="page-header-copy">
          <h2>Build #{build.number}</h2>
          <p className="subtle-text">{build.job_name || "Build"}</p>
        </div>
      </div>
      <section className="detail-panel" aria-label="Public build summary">
        <h3>Build Summary</h3>
        <div className="detail-grid">
          <div>
            <strong>Status</strong>
            <StatusBadge status={build.status} />
          </div>
          <div>
            <strong>Attempt</strong>
            <span>{build.attempt}</span>
          </div>
          <div>
            <strong>Created</strong>
            <span>{formatTime(build.created_at)}</span>
          </div>
          <div>
            <strong>Started</strong>
            <span>{formatTime(build.started_at)}</span>
          </div>
          <div>
            <strong>Completed</strong>
            <span>{formatTime(build.completed_at)}</span>
          </div>
        </div>
      </section>
      <section className="detail-panel" aria-label="Public build steps">
        <h3>Steps</h3>
        {!build.steps?.length && (
          <p className="empty">No step information is available.</p>
        )}
        {build.steps?.length ? (
          <table className="table">
            <thead>
              <tr>
                <th>Step</th>
                <th>Status</th>
                <th>Started</th>
                <th>Completed</th>
              </tr>
            </thead>
            <tbody>
              {build.steps.map((step) => (
                <tr key={step.index}>
                  <td>{step.name}</td>
                  <td>
                    <StatusBadge status={step.status} />
                  </td>
                  <td>{formatTime(step.started_at)}</td>
                  <td>{formatTime(step.completed_at)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        ) : null}
      </section>
    </div>
  );
}
