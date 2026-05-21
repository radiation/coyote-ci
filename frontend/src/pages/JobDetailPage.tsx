import { useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "react-router-dom";
import {
  getProject,
  getJob,
  listArtifactCatalog,
  listSourceCredentials,
  runJob,
  updateJob,
} from "../api";
import { BuildActivityRail } from "../components/ScopedBuildActivityPanels";
import {
  ManagedBuildImageFields,
  type ManagedBuildImageValue,
} from "../components/ManagedBuildImageFields";
import { jobBuildsQueryOptions } from "../queries/jobBuilds";
import { StatusBadge } from "../components/StatusBadge";
import type { Job } from "../types/job";
import { formatTime } from "../utils/time";

const MIN_PRIORITY = 1;
const MAX_PRIORITY = 10;

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

  const {
    data: latestArtifacts,
    isLoading: latestArtifactsLoading,
    error: latestArtifactsError,
  } = useQuery({
    queryKey: ["jobArtifacts", id],
    queryFn: () => listArtifactCatalog({ job_id: id!, limit: 1 }),
    enabled: Boolean(id),
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

  const sortedBuilds = sortBuildsByNewest(jobBuilds ?? []);
  const latestBuild = sortedBuilds[0] ?? job.latest_build ?? null;
  const latestSuccessfulBuild =
    sortedBuilds.find((build) => build.status === "success") ?? null;
  const latestArtifact = latestArtifacts?.[0] ?? null;

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
            {latestBuild && (
              <div className="detail-summary">
                <span>
                  <strong>Latest Build:</strong>{" "}
                  <Link to={`/builds/${latestBuild.id}`}>
                    {typeof latestBuild.build_number === "number"
                      ? `#${latestBuild.build_number}`
                      : latestBuild.id.slice(0, 8)}
                  </Link>
                </span>
                <span>
                  <StatusBadge status={latestBuild.status} />
                </span>
                <span>
                  <strong>Created:</strong> {formatTime(latestBuild.created_at)}
                </span>
                {latestSuccessfulBuild &&
                  latestSuccessfulBuild.id !== latestBuild.id && (
                    <span>
                      <strong>Latest Success:</strong>{" "}
                      <Link to={`/builds/${latestSuccessfulBuild.id}`}>
                        #{latestSuccessfulBuild.build_number ?? "—"}
                      </Link>
                    </span>
                  )}
              </div>
            )}

            {latestArtifactsLoading && <p>Loading latest artifacts…</p>}
            {latestArtifactsError && (
              <p className="error-text">
                Failed to load latest artifacts: {String(latestArtifactsError)}
              </p>
            )}
            {!latestArtifactsLoading &&
              !latestArtifactsError &&
              !latestArtifact && (
                <p className="subtle-text">No artifacts yet for this job.</p>
              )}
            {latestArtifact && (
              <div className="detail-summary">
                <span>
                  <strong>Latest Artifact:</strong>{" "}
                  <Link to={`/artifacts/${latestArtifact.id}`}>
                    {latestArtifact.name || latestArtifact.path}
                  </Link>
                </span>
                <span>
                  <strong>Build:</strong>{" "}
                  <Link to={`/builds/${latestArtifact.build_id}`}>
                    #{latestArtifact.build_number}
                  </Link>
                </span>
                <span>
                  <strong>Created:</strong>{" "}
                  {formatTime(latestArtifact.created_at)}
                </span>
              </div>
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

type PipelineMode = "inline" | "repo";

function JobDetailForm({ job, jobID }: { job: Job; jobID: string }) {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [name, setName] = useState(job.name);
  const [priority, setPriority] = useState(String(job.priority));
  const [repositoryURL, setRepositoryURL] = useState(job.repository_url);
  const [defaultRef, setDefaultRef] = useState(job.default_ref);
  const [pushEnabled, setPushEnabled] = useState(job.push_enabled);
  const [pushBranch, setPushBranch] = useState(job.push_branch ?? "");
  const [pipelineMode, setPipelineMode] = useState<PipelineMode>(
    job.pipeline_path ? "repo" : "inline",
  );
  const [pipelineYAML, setPipelineYAML] = useState(job.pipeline_yaml);
  const [pipelinePath, setPipelinePath] = useState(
    job.pipeline_path ?? ".coyote/pipeline.yml",
  );
  const [managedImage, setManagedImage] = useState<ManagedBuildImageValue>({
    enabled: job.managed_image?.enabled ?? false,
    managedImageName: job.managed_image?.managed_image_name ?? "go",
    pipelinePath: job.managed_image?.pipeline_path ?? ".coyote/pipeline.yml",
    writeCredentialID: job.managed_image?.write_credential_id ?? "",
    botBranchPrefix:
      job.managed_image?.bot_branch_prefix ?? "coyote/managed-image-refresh",
    commitAuthorName: job.managed_image?.commit_author_name ?? "Coyote CI Bot",
    commitAuthorEmail:
      job.managed_image?.commit_author_email ?? "bot@coyote-ci.local",
  });
  const [enabled, setEnabled] = useState(job.enabled);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const [successMessage, setSuccessMessage] = useState<string | null>(null);

  const { data: credentials = [], isLoading: credentialsLoading } = useQuery({
    queryKey: ["source-credentials"],
    queryFn: () => listSourceCredentials(),
  });

  const saveMutation = useMutation({
    mutationFn: (targetID: string) => {
      const managedImagePayload = managedImage.enabled
        ? {
            enabled: true,
            managed_image_name: managedImage.managedImageName.trim(),
            pipeline_path: managedImage.pipelinePath.trim(),
            write_credential_id: managedImage.writeCredentialID.trim(),
            bot_branch_prefix: managedImage.botBranchPrefix.trim(),
            commit_author_name: managedImage.commitAuthorName.trim(),
            commit_author_email: managedImage.commitAuthorEmail.trim(),
          }
        : null;

      const base = {
        name: name.trim(),
        priority: Number.parseInt(priority, 10),
        repository_url: repositoryURL.trim(),
        default_ref: defaultRef.trim(),
        push_enabled: pushEnabled,
        push_branch: pushEnabled ? pushBranch.trim() : "",
        enabled,
        managed_image: managedImagePayload,
      };

      if (pipelineMode === "inline") {
        return updateJob(targetID, {
          ...base,
          pipeline_yaml: pipelineYAML.trim(),
          pipeline_path: "",
        });
      }
      return updateJob(targetID, {
        ...base,
        pipeline_yaml: "",
        pipeline_path: pipelinePath.trim(),
      });
    },
    onMutate: () => {
      setErrorMessage(null);
      setSuccessMessage(null);
    },
    onSuccess: async (updated) => {
      setSuccessMessage("Job saved.");
      await queryClient.invalidateQueries({ queryKey: ["job", updated.id] });
      await queryClient.invalidateQueries({ queryKey: ["jobs"] });
    },
    onError: (mutationError) => {
      setErrorMessage(`Failed to save job: ${String(mutationError)}`);
    },
  });

  const runNowMutation = useMutation({
    mutationFn: (targetID: string) => runJob(targetID),
    onMutate: () => {
      setErrorMessage(null);
      setSuccessMessage(null);
    },
    onSuccess: (build) => {
      if (build.id) {
        navigate(`/builds/${build.id}`);
        return;
      }
      setSuccessMessage("Job run started.");
    },
    onError: (mutationError) => {
      setErrorMessage(`Failed to run job: ${String(mutationError)}`);
    },
  });

  const onSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();

    if (!name.trim() || !repositoryURL.trim() || !defaultRef.trim()) {
      setErrorMessage("Name, repository URL, and default ref are required.");
      return;
    }

    const parsedPriority = Number.parseInt(priority, 10);
    if (
      Number.isNaN(parsedPriority) ||
      parsedPriority < MIN_PRIORITY ||
      parsedPriority > MAX_PRIORITY
    ) {
      setErrorMessage("Priority must be a number from 1 to 10.");
      return;
    }

    if (pipelineMode === "inline" && !pipelineYAML.trim()) {
      setErrorMessage("Pipeline YAML is required.");
      return;
    }

    if (pipelineMode === "repo" && !pipelinePath.trim()) {
      setErrorMessage("Pipeline file path is required.");
      return;
    }

    if (
      managedImage.enabled &&
      (!managedImage.managedImageName.trim() ||
        !managedImage.pipelinePath.trim() ||
        !managedImage.writeCredentialID.trim())
    ) {
      setErrorMessage(
        "Managed build image name, pipeline path, and write credential are required when automation is enabled.",
      );
      return;
    }

    saveMutation.mutate(jobID);
  };

  const isSubmitting = saveMutation.isPending || runNowMutation.isPending;

  return (
    <>
      <form className="job-form" onSubmit={onSubmit}>
        <label htmlFor="job-name">Name</label>
        <input
          id="job-name"
          value={name}
          onChange={(event) => setName(event.target.value)}
          disabled={isSubmitting}
        />

        <label htmlFor="job-priority">Priority</label>
        <input
          id="job-priority"
          type="number"
          min={MIN_PRIORITY}
          max={MAX_PRIORITY}
          value={priority}
          onChange={(event) => setPriority(event.target.value)}
          disabled={isSubmitting}
        />

        <label htmlFor="job-repository-url">Repository URL</label>
        <input
          id="job-repository-url"
          value={repositoryURL}
          onChange={(event) => setRepositoryURL(event.target.value)}
          disabled={isSubmitting}
        />

        <label htmlFor="job-default-ref">Default Ref</label>
        <input
          id="job-default-ref"
          value={defaultRef}
          onChange={(event) => setDefaultRef(event.target.value)}
          disabled={isSubmitting}
        />

        <label className="checkbox-label" htmlFor="job-push-enabled">
          <input
            id="job-push-enabled"
            type="checkbox"
            checked={pushEnabled}
            onChange={(event) => setPushEnabled(event.target.checked)}
            disabled={isSubmitting}
          />
          Enable push trigger
        </label>

        <label htmlFor="job-push-branch">Push Branch</label>
        <input
          id="job-push-branch"
          value={pushBranch}
          onChange={(event) => setPushBranch(event.target.value)}
          disabled={isSubmitting}
          placeholder="main"
        />

        <fieldset disabled={isSubmitting}>
          <legend>Pipeline Source</legend>
          <label className="radio-label">
            <input
              type="radio"
              name="pipeline-mode"
              value="inline"
              checked={pipelineMode === "inline"}
              onChange={() => setPipelineMode("inline")}
            />
            Inline YAML
          </label>
          <label className="radio-label">
            <input
              type="radio"
              name="pipeline-mode"
              value="repo"
              checked={pipelineMode === "repo"}
              onChange={() => setPipelineMode("repo")}
            />
            File in repository
          </label>
        </fieldset>

        {pipelineMode === "inline" && (
          <>
            <label htmlFor="job-pipeline-yaml">Pipeline YAML</label>
            <textarea
              id="job-pipeline-yaml"
              value={pipelineYAML}
              onChange={(event) => setPipelineYAML(event.target.value)}
              rows={14}
              disabled={isSubmitting}
            />
          </>
        )}

        {pipelineMode === "repo" && (
          <>
            <label htmlFor="job-pipeline-path">Pipeline File Path</label>
            <input
              id="job-pipeline-path"
              value={pipelinePath}
              onChange={(event) => setPipelinePath(event.target.value)}
              disabled={isSubmitting}
              placeholder=".coyote/pipeline.yml"
            />
            <p className="subtle-text">
              Path to pipeline file inside the repository. Loaded at build time.
            </p>
          </>
        )}

        <ManagedBuildImageFields
          value={managedImage}
          onChange={(patch) =>
            setManagedImage((current) => ({ ...current, ...patch }))
          }
          credentials={credentials}
          credentialsLoading={credentialsLoading}
          disabled={isSubmitting}
        />

        <label className="checkbox-label" htmlFor="job-enabled">
          <input
            id="job-enabled"
            type="checkbox"
            checked={enabled}
            onChange={(event) => setEnabled(event.target.checked)}
            disabled={isSubmitting}
          />
          Enabled
        </label>

        <div className="job-form-actions">
          <button type="submit" disabled={isSubmitting}>
            {saveMutation.isPending ? "Saving…" : "Save Job"}
          </button>
          <button
            type="button"
            className="secondary-button"
            onClick={() => runNowMutation.mutate(jobID)}
            disabled={isSubmitting}
          >
            {runNowMutation.isPending ? "Running…" : "Run Now"}
          </button>
        </div>
      </form>

      {successMessage && <p className="success-text">{successMessage}</p>}
      {errorMessage && <p className="error-text">{errorMessage}</p>}
    </>
  );
}
