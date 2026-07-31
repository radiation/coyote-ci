import { useState, type FormEvent } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { createJob, listProjects, listSourceCredentials } from "../api";
import {
  ManagedBuildImageFields,
  type ManagedBuildImageValue,
} from "../components/ManagedBuildImageFields";

const MIN_PRIORITY = 1;
const MAX_PRIORITY = 10;

type PipelineMode = "inline" | "repo";

const DEFAULT_PIPELINE_YAML = `version: 1
steps:
  - name: test
    run: go test ./...
`;

export function JobCreatePage() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const [projectID, setProjectID] = useState("");
  const [name, setName] = useState("");
  const [priority, setPriority] = useState("5");
  const [repositoryURL, setRepositoryURL] = useState("");
  const [defaultRef, setDefaultRef] = useState("main");
  const [pushEnabled, setPushEnabled] = useState(false);
  const [pullRequestEnabled, setPullRequestEnabled] = useState(false);
  const [pushBranch, setPushBranch] = useState("main");
  const [pipelineMode, setPipelineMode] = useState<PipelineMode>("inline");
  const [pipelineYAML, setPipelineYAML] = useState(DEFAULT_PIPELINE_YAML);
  const [pipelinePath, setPipelinePath] = useState(".coyote/pipeline.yml");
  const [managedImage, setManagedImage] = useState<ManagedBuildImageValue>({
    enabled: false,
    managedImageName: "go",
    pipelinePath: ".coyote/pipeline.yml",
    writeCredentialID: "",
    botBranchPrefix: "coyote/managed-image-refresh",
    commitAuthorName: "Coyote CI Bot",
    commitAuthorEmail: "bot@coyote-ci.local",
  });
  const [enabled, setEnabled] = useState(true);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);

  const { data: credentials = [], isLoading: credentialsLoading } = useQuery({
    queryKey: ["source-credentials"],
    queryFn: () => listSourceCredentials(),
  });

  const { data: projects = [], isLoading: projectsLoading } = useQuery({
    queryKey: ["projects"],
    queryFn: () => listProjects(),
  });

  const requestedProjectID = searchParams.get("project_id")?.trim() ?? "";
  const hasRequestedProject = projects.some(
    (project) => project.id === requestedProjectID,
  );
  const selectedProjectID =
    projectID ||
    (hasRequestedProject ? requestedProjectID : "") ||
    projects[0]?.id ||
    "";

  const createMutation = useMutation({
    mutationFn: createJob,
    onMutate: () => {
      setErrorMessage(null);
    },
    onSuccess: (job) => {
      navigate(`/jobs/${job.id}`);
    },
    onError: (mutationError) => {
      setErrorMessage(`Failed to create job: ${String(mutationError)}`);
    },
  });

  const onSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();

    const trimmedProjectID = selectedProjectID.trim();
    const trimmedName = name.trim();
    const parsedPriority = Number.parseInt(priority, 10);
    const trimmedRepositoryURL = repositoryURL.trim();
    const trimmedDefaultRef = defaultRef.trim();
    const trimmedPushBranch = pushBranch.trim();

    if (
      !trimmedProjectID ||
      !trimmedName ||
      !trimmedRepositoryURL ||
      !trimmedDefaultRef
    ) {
      setErrorMessage(
        "Project, name, repository URL, and default ref are required.",
      );
      return;
    }

    if (
      Number.isNaN(parsedPriority) ||
      parsedPriority < MIN_PRIORITY ||
      parsedPriority > MAX_PRIORITY
    ) {
      setErrorMessage("Priority must be a number from 1 to 10.");
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
      : undefined;

    if (pipelineMode === "inline") {
      const trimmedYAML = pipelineYAML.trim();
      if (!trimmedYAML) {
        setErrorMessage("Pipeline YAML is required.");
        return;
      }
      createMutation.mutate({
        project_id: trimmedProjectID,
        name: trimmedName,
        priority: parsedPriority,
        repository_url: trimmedRepositoryURL,
        default_ref: trimmedDefaultRef,
        push_enabled: pushEnabled,
        pull_request_enabled: pullRequestEnabled,
        push_branch: pushEnabled ? trimmedPushBranch : "",
        pipeline_yaml: trimmedYAML,
        managed_image: managedImagePayload,
        enabled,
      });
    } else {
      const trimmedPath = pipelinePath.trim();
      if (!trimmedPath) {
        setErrorMessage("Pipeline file path is required.");
        return;
      }
      createMutation.mutate({
        project_id: trimmedProjectID,
        name: trimmedName,
        priority: parsedPriority,
        repository_url: trimmedRepositoryURL,
        default_ref: trimmedDefaultRef,
        push_enabled: pushEnabled,
        pull_request_enabled: pullRequestEnabled,
        push_branch: pushEnabled ? trimmedPushBranch : "",
        pipeline_path: trimmedPath,
        managed_image: managedImagePayload,
        enabled,
      });
    }
  };

  return (
    <>
      <Link to="/jobs">← Back to jobs</Link>
      <h2>Create Job</h2>
      <p className="subtle-text">
        Define a reusable pipeline. Builds are created by running a job.
      </p>

      <form className="job-form" onSubmit={onSubmit}>
        <label htmlFor="job-project-id">Project</label>
        <select
          id="job-project-id"
          value={selectedProjectID}
          onChange={(event) => setProjectID(event.target.value)}
          disabled={createMutation.isPending || projectsLoading}
        >
          <option value="">Select a project</option>
          {projects.map((project) => (
            <option key={project.id} value={project.id}>
              {project.name} ({project.slug})
            </option>
          ))}
        </select>
        {projects.length === 0 && !projectsLoading && (
          <p className="subtle-text">
            No projects found.{" "}
            <Link to="/projects">Create a project first.</Link>
          </p>
        )}

        <label htmlFor="job-name">Name</label>
        <input
          id="job-name"
          value={name}
          onChange={(event) => setName(event.target.value)}
          disabled={createMutation.isPending}
          placeholder="backend-ci"
        />

        <label htmlFor="job-priority">Priority</label>
        <input
          id="job-priority"
          type="number"
          min={MIN_PRIORITY}
          max={MAX_PRIORITY}
          value={priority}
          onChange={(event) => setPriority(event.target.value)}
          disabled={createMutation.isPending}
        />
        <p className="subtle-text">
          Higher priority builds are dispatched first. Default is 5.
        </p>

        <label htmlFor="job-repository-url">Repository URL</label>
        <input
          id="job-repository-url"
          value={repositoryURL}
          onChange={(event) => setRepositoryURL(event.target.value)}
          disabled={createMutation.isPending}
          placeholder="https://github.com/org/repo.git"
        />

        <label htmlFor="job-default-ref">Default Ref</label>
        <input
          id="job-default-ref"
          value={defaultRef}
          onChange={(event) => setDefaultRef(event.target.value)}
          disabled={createMutation.isPending}
          placeholder="main"
        />

        <label className="checkbox-label" htmlFor="job-push-enabled">
          <input
            id="job-push-enabled"
            type="checkbox"
            checked={pushEnabled}
            onChange={(event) => setPushEnabled(event.target.checked)}
            disabled={createMutation.isPending}
          />
          Enable push trigger
        </label>

        {pushEnabled && (
          <>
            <label htmlFor="job-push-branch">Push Branch</label>
            <input
              id="job-push-branch"
              value={pushBranch}
              onChange={(event) => setPushBranch(event.target.value)}
              disabled={createMutation.isPending}
              placeholder="main"
            />
          </>
        )}

        <label className="checkbox-label" htmlFor="job-pull-request-enabled">
          <input
            id="job-pull-request-enabled"
            type="checkbox"
            checked={pullRequestEnabled}
            onChange={(event) => setPullRequestEnabled(event.target.checked)}
            disabled={createMutation.isPending}
          />
          Enable pull request trigger
        </label>

        <fieldset disabled={createMutation.isPending}>
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
              disabled={createMutation.isPending}
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
              disabled={createMutation.isPending}
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
          disabled={createMutation.isPending}
        />

        <label className="checkbox-label" htmlFor="job-enabled">
          <input
            id="job-enabled"
            type="checkbox"
            checked={enabled}
            onChange={(event) => setEnabled(event.target.checked)}
            disabled={createMutation.isPending}
          />
          Enabled
        </label>

        <button type="submit" disabled={createMutation.isPending}>
          {createMutation.isPending ? "Creating…" : "Create Job"}
        </button>
      </form>

      {errorMessage && <p className="error-text">{errorMessage}</p>}
    </>
  );
}
