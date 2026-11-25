import { useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { listSourceCredentials, updateJob } from "../api";
import {
  ManagedBuildImageFields,
  type ManagedBuildImageValue,
} from "../components/ManagedBuildImageFields";
import type { Job } from "../types/job";

const MIN_PRIORITY = 1;
const MAX_PRIORITY = 10;

type PipelineMode = "inline" | "repo";

function formValues(job: Job) {
  const pipelineMode: PipelineMode = job.pipeline_path ? "repo" : "inline";
  const managedImage: ManagedBuildImageValue = {
    enabled: job.managed_image?.enabled ?? false,
    managedImageName: job.managed_image?.managed_image_name ?? "go",
    pipelinePath: job.managed_image?.pipeline_path ?? ".coyote/pipeline.yml",
    writeCredentialID: job.managed_image?.write_credential_id ?? "",
    botBranchPrefix:
      job.managed_image?.bot_branch_prefix ?? "coyote/managed-image-refresh",
    commitAuthorName: job.managed_image?.commit_author_name ?? "Coyote CI Bot",
    commitAuthorEmail:
      job.managed_image?.commit_author_email ?? "bot@coyote-ci.local",
  };
  return {
    name: job.name,
    priority: String(job.priority),
    repositoryURL: job.repository_url,
    defaultRef: job.default_ref,
    pushEnabled: job.push_enabled,
    pullRequestEnabled: job.pull_request_enabled,
    pushBranch: job.push_branch ?? "",
    pipelineMode,
    pipelineYAML: job.pipeline_yaml,
    pipelinePath: job.pipeline_path ?? ".coyote/pipeline.yml",
    managedImage,
    enabled: job.enabled,
  };
}

export function JobConfiguration({
  job,
  active,
}: {
  job: Job;
  active: boolean;
}) {
  const [draft, setDraft] = useState<ReturnType<typeof formValues> | null>(
    null,
  );
  if (!active) return null;
  const values = draft ?? formValues(job);
  return (
    <section
      id="job-configuration"
      className="detail-panel"
      aria-label="Job configuration"
    >
      <h3>Job Configuration</h3>
      <JobDetailForm
        jobID={job.id}
        values={values}
        change={(patch) =>
          setDraft((current) => ({ ...(current ?? values), ...patch }))
        }
        onSaved={() => setDraft(null)}
      />
    </section>
  );
}

function JobDetailForm({
  jobID,
  values,
  change,
  onSaved,
}: {
  jobID: string;
  values: ReturnType<typeof formValues>;
  change: (patch: Partial<ReturnType<typeof formValues>>) => void;
  onSaved: () => void;
}) {
  const queryClient = useQueryClient();
  const {
    name,
    priority,
    repositoryURL,
    defaultRef,
    pushEnabled,
    pullRequestEnabled,
    pushBranch,
    pipelineMode,
    pipelineYAML,
    pipelinePath,
    managedImage,
    enabled,
  } = values;
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const [successMessage, setSuccessMessage] = useState("");

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
        pull_request_enabled: pullRequestEnabled,
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
      setSuccessMessage("");
    },
    onSuccess: async (updated) => {
      queryClient.setQueryData(["job", updated.id], updated);
      onSaved();
      setSuccessMessage("Job saved.");
      await queryClient.invalidateQueries({
        queryKey: ["projectJobs", updated.project_id],
      });
      await queryClient.invalidateQueries({ queryKey: ["jobs"] });
    },
    onError: (mutationError) => {
      setErrorMessage(`Failed to save job: ${String(mutationError)}`);
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

  const isSubmitting = saveMutation.isPending;

  return (
    <>
      <form className="job-form" onSubmit={onSubmit}>
        <label htmlFor="job-name">Name</label>
        <input
          id="job-name"
          value={name}
          onChange={(event) => change({ name: event.target.value })}
          disabled={isSubmitting}
        />

        <label htmlFor="job-priority">Priority</label>
        <input
          id="job-priority"
          type="number"
          min={MIN_PRIORITY}
          max={MAX_PRIORITY}
          value={priority}
          onChange={(event) => change({ priority: event.target.value })}
          disabled={isSubmitting}
        />

        <label htmlFor="job-repository-url">Repository URL</label>
        <input
          id="job-repository-url"
          value={repositoryURL}
          onChange={(event) => change({ repositoryURL: event.target.value })}
          disabled={isSubmitting}
        />

        <label htmlFor="job-default-ref">Default Ref</label>
        <input
          id="job-default-ref"
          value={defaultRef}
          onChange={(event) => change({ defaultRef: event.target.value })}
          disabled={isSubmitting}
        />

        <label className="checkbox-label" htmlFor="job-push-enabled">
          <input
            id="job-push-enabled"
            type="checkbox"
            checked={pushEnabled}
            onChange={(event) => change({ pushEnabled: event.target.checked })}
            disabled={isSubmitting}
          />
          Enable push trigger
        </label>

        <label htmlFor="job-push-branch">Push Branch</label>
        <input
          id="job-push-branch"
          value={pushBranch}
          onChange={(event) => change({ pushBranch: event.target.value })}
          disabled={isSubmitting}
          placeholder="main"
        />

        <label className="checkbox-label" htmlFor="job-pull-request-enabled">
          <input
            id="job-pull-request-enabled"
            type="checkbox"
            checked={pullRequestEnabled}
            onChange={(event) =>
              change({ pullRequestEnabled: event.target.checked })
            }
            disabled={isSubmitting}
          />
          Enable pull request trigger
        </label>

        <fieldset disabled={isSubmitting}>
          <legend>Pipeline Source</legend>
          <label className="radio-label">
            <input
              type="radio"
              name="pipeline-mode"
              value="inline"
              checked={pipelineMode === "inline"}
              onChange={() => change({ pipelineMode: "inline" })}
            />
            Inline YAML
          </label>
          <label className="radio-label">
            <input
              type="radio"
              name="pipeline-mode"
              value="repo"
              checked={pipelineMode === "repo"}
              onChange={() => change({ pipelineMode: "repo" })}
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
              onChange={(event) => change({ pipelineYAML: event.target.value })}
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
              onChange={(event) => change({ pipelinePath: event.target.value })}
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
            change({ managedImage: { ...managedImage, ...patch } })
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
            onChange={(event) => change({ enabled: event.target.checked })}
            disabled={isSubmitting}
          />
          Enabled
        </label>

        <div className="job-form-actions">
          <button type="submit" disabled={isSubmitting}>
            {saveMutation.isPending ? "Saving…" : "Save Job"}
          </button>
        </div>
      </form>

      {successMessage && (
        <p role="status" className="success-text">
          {successMessage}
        </p>
      )}
      {errorMessage && <p className="error-text">{errorMessage}</p>}
    </>
  );
}
