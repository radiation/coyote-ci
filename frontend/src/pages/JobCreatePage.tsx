import { useState, type FormEvent } from "react";
import { useMutation } from "@tanstack/react-query";
import { Link, useNavigate } from "react-router-dom";
import { createJob } from "../api";

type PipelineMode = "inline" | "repo";

const DEFAULT_PIPELINE_YAML = `version: 1
steps:
  - name: test
    run: go test ./...
`;

export function JobCreatePage() {
  const navigate = useNavigate();
  const [projectID, setProjectID] = useState("project-1");
  const [name, setName] = useState("");
  const [repositoryURL, setRepositoryURL] = useState("");
  const [defaultRef, setDefaultRef] = useState("main");
  const [pushEnabled, setPushEnabled] = useState(false);
  const [pushBranch, setPushBranch] = useState("main");
  const [pipelineMode, setPipelineMode] = useState<PipelineMode>("inline");
  const [pipelineYAML, setPipelineYAML] = useState(DEFAULT_PIPELINE_YAML);
  const [pipelinePath, setPipelinePath] = useState(".coyote/pipeline.yml");
  const [enabled, setEnabled] = useState(true);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);

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

    const trimmedProjectID = projectID.trim();
    const trimmedName = name.trim();
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
        "Project ID, name, repository URL, and default ref are required.",
      );
      return;
    }

    if (pipelineMode === "inline") {
      const trimmedYAML = pipelineYAML.trim();
      if (!trimmedYAML) {
        setErrorMessage("Pipeline YAML is required.");
        return;
      }
      createMutation.mutate({
        project_id: trimmedProjectID,
        name: trimmedName,
        repository_url: trimmedRepositoryURL,
        default_ref: trimmedDefaultRef,
        push_enabled: pushEnabled,
        push_branch: pushEnabled ? trimmedPushBranch : "",
        pipeline_yaml: trimmedYAML,
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
        repository_url: trimmedRepositoryURL,
        default_ref: trimmedDefaultRef,
        push_enabled: pushEnabled,
        push_branch: pushEnabled ? trimmedPushBranch : "",
        pipeline_path: trimmedPath,
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
        <label htmlFor="job-project-id">Project ID</label>
        <input
          id="job-project-id"
          value={projectID}
          onChange={(event) => setProjectID(event.target.value)}
          disabled={createMutation.isPending}
        />

        <label htmlFor="job-name">Name</label>
        <input
          id="job-name"
          value={name}
          onChange={(event) => setName(event.target.value)}
          disabled={createMutation.isPending}
          placeholder="backend-ci"
        />

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
