import { useQuery } from "@tanstack/react-query";
import { Link, useParams } from "react-router-dom";
import {
  artifactDownloadURL,
  getBuildArtifacts,
  getProject,
  getJob,
} from "../api";
import { BuildActivityRail } from "../components/ScopedBuildActivityPanels";
import { jobBuildsQueryOptions } from "../queries/jobBuilds";
import { StatusBadge } from "../components/StatusBadge";
import {
  artifactSecondaryPath,
  artifactTitle,
  formatChecksumDisplay,
  formatFileSize,
} from "../utils/format";
import { formatTime } from "../utils/time";
import { JobDetailForm } from "./JobDetailForm";

function sortBuildsByNewest<
  T extends {
    finished_at: string | null;
    started_at: string | null;
    queued_at: string | null;
    created_at: string;
  },
>(builds: T[]): T[] {
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

function artifactCountLabel(count: number): string {
  return `${count} artifact${count === 1 ? "" : "s"}`;
}

export function JobDetailPage() {
  const { id } = useParams<{ id: string }>();

  const {
    data: job,
    isLoading,
    error,
    dataUpdatedAt,
  } = useQuery({
    queryKey: ["job", id],
    queryFn: () => getJob(id!),
    enabled: Boolean(id),
  });

  const { data: project } = useQuery({
    queryKey: ["project", job?.project_id],
    queryFn: () => getProject(job!.project_id),
    enabled: Boolean(job?.project_id),
  });

  const {
    data: jobBuilds,
    isLoading: jobBuildsLoading,
    error: jobBuildsError,
  } = useQuery(
    id
      ? jobBuildsQueryOptions(id)
      : {
          queryKey: ["jobBuilds", "disabled", "detail"],
          queryFn: async () => [],
          enabled: false,
        },
  );

  const sortedBuilds = sortBuildsByNewest(jobBuilds ?? []);
  const latestBuild = sortedBuilds[0] ?? job?.latest_build ?? null;
  const latestSuccessfulBuild =
    sortedBuilds.find((build) => build.status === "success") ?? null;

  const {
    data: latestSuccessfulArtifacts,
    isLoading: latestSuccessfulArtifactsLoading,
    error: latestSuccessfulArtifactsError,
  } = useQuery({
    queryKey: ["jobLatestSuccessfulArtifacts", latestSuccessfulBuild?.id],
    queryFn: () => getBuildArtifacts(latestSuccessfulBuild!.id),
    enabled: Boolean(latestSuccessfulBuild?.id),
  });

  if (isLoading) {
    return <p>Loading job…</p>;
  }

  if (error) {
    return <p className="error-text">Failed to load job: {String(error)}</p>;
  }

  if (!job || !id) {
    return <p className="error-text">Job not found.</p>;
  }

  const latestOutputArtifacts = (latestSuccessfulArtifacts ?? []).slice(0, 3);
  const latestOutputArtifactCount = latestSuccessfulArtifacts?.length ?? 0;
  const latestOutputOverflow = Math.max(
    latestOutputArtifactCount - latestOutputArtifacts.length,
    0,
  );
  const latestBuildLabel =
    typeof latestBuild?.build_number === "number"
      ? `#${latestBuild.build_number}`
      : (latestBuild?.id.slice(0, 8) ?? "—");
  const latestSuccessfulBuildLabel =
    typeof latestSuccessfulBuild?.build_number === "number"
      ? `#${latestSuccessfulBuild.build_number}`
      : (latestSuccessfulBuild?.id.slice(0, 8) ?? "—");

  return (
    <>
      <Link to="/jobs">← Back to jobs</Link>
      <div className="detail-page-with-rail">
        <div className="detail-main-column">
          <div className="page-header-row">
            <div className="page-header-copy">
              <h2>Job: {job.name}</h2>
              <p className="subtle-text">ID: {job.id}</p>
            </div>
            <div className="page-header-actions">
              <Link
                to={`/builds?project_id=${encodeURIComponent(job.project_id)}`}
              >
                View Project Builds
              </Link>
              <Link
                to={`/artifacts?project_id=${encodeURIComponent(job.project_id)}&job_id=${encodeURIComponent(job.id)}`}
              >
                Browse Job Artifacts
              </Link>
              <a href="#job-configuration">Edit Configuration</a>
            </div>
          </div>

          <section className="detail-panel" aria-label="Job summary">
            <h3>Job Summary</h3>
            <div className="detail-grid">
              <div>
                <strong>Project</strong>
                <span>
                  {project ? (
                    <Link to={`/projects/${project.id}`}>{project.name}</Link>
                  ) : (
                    job.project_id
                  )}
                </span>
              </div>
              <div>
                <strong>Enabled</strong>
                <span>{job.enabled ? "Enabled" : "Disabled"}</span>
              </div>
              <div>
                <strong>Priority</strong>
                <span>{job.priority}</span>
              </div>
              <div>
                <strong>Push Trigger</strong>
                <span>{job.push_enabled ? "Enabled" : "Disabled"}</span>
              </div>
              <div>
                <strong>Push Branch</strong>
                <span>
                  {job.push_enabled ? job.push_branch || "Any branch" : "—"}
                </span>
              </div>
              <div>
                <strong>Created</strong>
                <span>{formatTime(job.created_at)}</span>
              </div>
              <div>
                <strong>Updated</strong>
                <span>{formatTime(job.updated_at)}</span>
              </div>
              <div>
                <strong>Last Loaded</strong>
                <span>
                  {dataUpdatedAt > 0
                    ? formatTime(new Date(dataUpdatedAt).toISOString())
                    : "—"}
                </span>
              </div>
            </div>
          </section>

          <section className="detail-panel" aria-label="Source and pipeline">
            <h3>Source and Pipeline</h3>
            <div className="detail-grid">
              <div>
                <strong>Repository</strong>
                <span>{job.repository_url}</span>
              </div>
              <div>
                <strong>Default Ref</strong>
                <span>{job.default_ref}</span>
              </div>
              <div>
                <strong>Pipeline Source</strong>
                <span>
                  {job.pipeline_path ? "Repository file" : "Inline YAML"}
                </span>
              </div>
              <div>
                <strong>Pipeline Path</strong>
                <span>{job.pipeline_path || "—"}</span>
              </div>
              <div>
                <strong>Managed Build Image</strong>
                <span>
                  {job.managed_image?.enabled ? "Enabled" : "Disabled"}
                </span>
              </div>
              <div>
                <strong>Managed Image Name</strong>
                <span>{job.managed_image?.managed_image_name || "—"}</span>
              </div>
            </div>
            <p className="subtle-text">
              Internal push events can be sent to POST /events/push with
              repository_url, ref, and commit_sha.
            </p>
          </section>

          <section className="detail-panel" aria-label="Latest outputs">
            <h3>Latest Outputs</h3>

            {jobBuildsLoading && <p>Loading latest builds…</p>}
            {jobBuildsError && (
              <p className="error-text">
                Failed to load latest builds: {String(jobBuildsError)}
              </p>
            )}
            {!jobBuildsLoading && !jobBuildsError && !latestBuild && (
              <p className="subtle-text">No builds yet for this job.</p>
            )}
            {latestBuild && !latestSuccessfulBuild && (
              <>
                <div className="detail-summary">
                  <span>
                    <strong>Build</strong>{" "}
                    <Link to={`/builds/${latestBuild.id}`}>
                      {latestBuildLabel}
                    </Link>
                  </span>
                  <span>
                    <StatusBadge status={latestBuild.status} />
                  </span>
                  <span>Created {formatTime(latestBuild.created_at)}</span>
                </div>
                <div className="detail-actions-row">
                  <Link to={`/builds/${latestBuild.id}`}>
                    View Latest Build
                  </Link>
                  <Link
                    to={`/artifacts?project_id=${encodeURIComponent(job.project_id)}&job_id=${encodeURIComponent(job.id)}`}
                  >
                    Browse Job Artifacts
                  </Link>
                </div>
              </>
            )}

            {latestBuild && !latestSuccessfulBuild && (
              <p className="subtle-text">No successful build outputs yet.</p>
            )}

            {latestSuccessfulBuild && latestSuccessfulArtifactsLoading && (
              <p>Loading latest successful build outputs…</p>
            )}
            {latestSuccessfulArtifactsError && (
              <p className="error-text">
                Failed to load latest successful build outputs:{" "}
                {String(latestSuccessfulArtifactsError)}
              </p>
            )}
            {latestSuccessfulBuild &&
              !latestSuccessfulArtifactsLoading &&
              !latestSuccessfulArtifactsError && (
                <>
                  <div className="detail-summary">
                    <span>
                      <strong>Build</strong>{" "}
                      <Link to={`/builds/${latestSuccessfulBuild.id}`}>
                        {latestSuccessfulBuildLabel}
                      </Link>
                    </span>
                    <span>
                      <StatusBadge status={latestSuccessfulBuild.status} />
                    </span>
                    <span>{artifactCountLabel(latestOutputArtifactCount)}</span>
                    <span>
                      Finished{" "}
                      {formatTime(
                        latestSuccessfulBuild.finished_at ??
                          latestSuccessfulBuild.created_at,
                      )}
                    </span>
                  </div>

                  {latestBuild.id !== latestSuccessfulBuild.id && (
                    <p className="subtle-text">
                      Latest build{" "}
                      <Link to={`/builds/${latestBuild.id}`}>
                        {latestBuildLabel}
                      </Link>{" "}
                      is <StatusBadge status={latestBuild.status} />.
                    </p>
                  )}

                  <div className="detail-actions-row">
                    <Link to={`/builds/${latestSuccessfulBuild.id}`}>
                      View Latest Build
                    </Link>
                    <Link
                      to={`/artifacts?project_id=${encodeURIComponent(job.project_id)}&job_id=${encodeURIComponent(job.id)}`}
                    >
                      Browse Job Artifacts
                    </Link>
                    <Link
                      to={`/artifacts/logical?project_id=${encodeURIComponent(job.project_id)}&job_id=${encodeURIComponent(job.id)}`}
                    >
                      Open Release View
                    </Link>
                  </div>

                  {latestOutputArtifacts.length === 0 ? (
                    <p className="subtle-text">
                      Latest successful build did not publish artifacts.
                    </p>
                  ) : (
                    <div className="job-latest-outputs-list" role="list">
                      {latestOutputArtifacts.map((artifact) => (
                        <article
                          key={artifact.id}
                          className="job-latest-output-item"
                          role="listitem"
                        >
                          <div className="job-latest-output-copy artifact-path">
                            <div className="artifact-catalog-primary">
                              <Link to={`/artifacts/${artifact.id}`}>
                                {artifactTitle(artifact)}
                              </Link>
                              {artifactSecondaryPath(artifact) && (
                                <div className="subtle-text artifact-mono">
                                  {artifactSecondaryPath(artifact)}
                                </div>
                              )}
                            </div>
                            <div className="job-latest-output-meta subtle-text">
                              <span>{formatFileSize(artifact.size_bytes)}</span>
                              {artifact.checksum_sha256 && (
                                <span
                                  className="artifact-mono artifact-checksum-value"
                                  title={artifact.checksum_sha256}
                                >
                                  sha256{" "}
                                  {formatChecksumDisplay(
                                    artifact.checksum_sha256,
                                  )}
                                </span>
                              )}
                              <span>{formatTime(artifact.created_at)}</span>
                            </div>
                          </div>
                          <div className="job-latest-output-actions">
                            <div className="artifact-actions">
                              <Link to={`/artifacts/${artifact.id}`}>Open</Link>
                              <a
                                href={artifactDownloadURL(
                                  artifact.download_url_path,
                                )}
                              >
                                Download
                              </a>
                            </div>
                          </div>
                        </article>
                      ))}
                    </div>
                  )}

                  {latestOutputOverflow > 0 && (
                    <p className="subtle-text">
                      Showing {latestOutputArtifacts.length} of{" "}
                      {latestOutputArtifactCount} artifacts from the latest
                      successful build.
                    </p>
                  )}
                </>
              )}
          </section>

          <section
            id="job-configuration"
            className="detail-panel"
            aria-label="Job configuration"
          >
            <h3>Job Configuration</h3>
            <JobDetailForm
              key={`${job.id}:${job.updated_at}`}
              job={job}
              jobID={id}
            />
          </section>
        </div>
        <BuildActivityRail scope={{ type: "job", jobId: id }} />
      </div>
    </>
  );
}
