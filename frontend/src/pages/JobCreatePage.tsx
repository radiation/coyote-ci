import { useState, type FormEvent } from 'react';
import { useMutation } from '@tanstack/react-query';
import { Link, useNavigate } from 'react-router-dom';
import { createJob } from '../api';

const DEFAULT_PIPELINE_YAML = `version: 1
steps:
  - name: test
    run: go test ./...
`;

export function JobCreatePage() {
  const navigate = useNavigate();
  const [projectID, setProjectID] = useState('project-1');
  const [name, setName] = useState('');
  const [repositoryURL, setRepositoryURL] = useState('');
  const [defaultRef, setDefaultRef] = useState('main');
  const [commitSHA, setCommitSHA] = useState('');
  const [pipelinePath, setPipelinePath] = useState('');
  const [pushEnabled, setPushEnabled] = useState(false);
  const [pushBranch, setPushBranch] = useState('main');
  const [pipelineYAML, setPipelineYAML] = useState(DEFAULT_PIPELINE_YAML);
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

  const onCreatePathJob = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();

    const trimmedProjectID = projectID.trim();
    const trimmedName = name.trim();
    const trimmedRepositoryURL = repositoryURL.trim();
    const trimmedDefaultRef = defaultRef.trim();
    const trimmedCommitSHA = commitSHA.trim();
    const trimmedPipelinePath = pipelinePath.trim();
    const trimmedPushBranch = pushBranch.trim();

    if (!trimmedProjectID || !trimmedName || !trimmedRepositoryURL) {
      setErrorMessage('Project ID, job name, and repository URL are required.');
      return;
    }
    if (!trimmedDefaultRef && !trimmedCommitSHA) {
      setErrorMessage('Ref or commit SHA is required.');
      return;
    }

    createMutation.mutate({
      project_id: trimmedProjectID,
      name: trimmedName,
      repository_url: trimmedRepositoryURL,
      ...(trimmedDefaultRef ? { default_ref: trimmedDefaultRef } : {}),
      ...(trimmedCommitSHA ? { default_commit_sha: trimmedCommitSHA } : {}),
      ...(trimmedPipelinePath ? { pipeline_path: trimmedPipelinePath } : {}),
      push_enabled: pushEnabled,
      push_branch: pushEnabled ? trimmedPushBranch : '',
      enabled,
    });
  };

  const onCreateInlineJob = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();

    const trimmedProjectID = projectID.trim();
    const trimmedName = name.trim();
    const trimmedRepositoryURL = repositoryURL.trim();
    const trimmedDefaultRef = defaultRef.trim();
    const trimmedCommitSHA = commitSHA.trim();
    const trimmedPushBranch = pushBranch.trim();
    const trimmedPipelineYAML = pipelineYAML.trim();

    if (!trimmedProjectID || !trimmedName || !trimmedRepositoryURL) {
      setErrorMessage('Project ID, job name, and repository URL are required.');
      return;
    }
    if (!trimmedDefaultRef && !trimmedCommitSHA) {
      setErrorMessage('Ref or commit SHA is required.');
      return;
    }
    if (!trimmedPipelineYAML) {
      setErrorMessage('Pipeline YAML is required for inline jobs.');
      return;
    }

    createMutation.mutate({
      project_id: trimmedProjectID,
      name: trimmedName,
      repository_url: trimmedRepositoryURL,
      ...(trimmedDefaultRef ? { default_ref: trimmedDefaultRef } : {}),
      ...(trimmedCommitSHA ? { default_commit_sha: trimmedCommitSHA } : {}),
      pipeline_yaml: trimmedPipelineYAML,
      push_enabled: pushEnabled,
      push_branch: pushEnabled ? trimmedPushBranch : '',
      enabled,
    });
  };

  const isSubmitting = createMutation.isPending;

  return (
    <>
      <Link to="/jobs">← Back to jobs</Link>
      <h2>Create Job Invocation</h2>
      <p className="subtle-text">Primary: store a job using repository pipeline YAML path. Secondary: store inline YAML.</p>

      <label htmlFor="job-name">Job Name</label>
      <input
        id="job-name"
        value={name}
        onChange={(event) => setName(event.target.value)}
        disabled={isSubmitting}
        placeholder="backend-ci"
      />

      <form className="job-form" onSubmit={onCreatePathJob}>
        <h3>Create Job With Repo Pipeline Path</h3>
        <label htmlFor="job-project-id">Project ID</label>
        <input
          id="job-project-id"
          value={projectID}
          onChange={(event) => setProjectID(event.target.value)}
          disabled={isSubmitting}
        />

        <label htmlFor="job-repository-url">Repository URL</label>
        <input
          id="job-repository-url"
          value={repositoryURL}
          onChange={(event) => setRepositoryURL(event.target.value)}
          disabled={isSubmitting}
          placeholder="https://github.com/org/repo.git"
        />

        <label htmlFor="job-default-ref">Ref</label>
        <input
          id="job-default-ref"
          value={defaultRef}
          onChange={(event) => setDefaultRef(event.target.value)}
          disabled={isSubmitting}
          placeholder="main"
        />

        <label htmlFor="job-commit-sha">Commit SHA</label>
        <input
          id="job-commit-sha"
          value={commitSHA}
          onChange={(event) => setCommitSHA(event.target.value)}
          disabled={isSubmitting}
          placeholder="Optional commit SHA"
        />

        <label htmlFor="job-pipeline-path">Pipeline YAML Path</label>
        <input
          id="job-pipeline-path"
          value={pipelinePath}
          onChange={(event) => setPipelinePath(event.target.value)}
          disabled={isSubmitting}
          placeholder=".coyote/pipeline.yml"
        />
        <p className="subtle-text">Path to the pipeline YAML file inside the repository. If omitted, backend default path is used.</p>

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

        <button type="submit" disabled={isSubmitting}>
          {createMutation.isPending ? 'Creating…' : 'Create Job With Repo Path'}
        </button>
      </form>

      <details>
        <summary>Secondary: Create Saved Inline YAML Job</summary>
        <form className="job-form" onSubmit={onCreateInlineJob}>

          <label htmlFor="job-pipeline-yaml">Pipeline YAML</label>
          <textarea
            id="job-pipeline-yaml"
            value={pipelineYAML}
            onChange={(event) => setPipelineYAML(event.target.value)}
            rows={14}
            disabled={isSubmitting}
          />

          <button type="submit" disabled={isSubmitting}>
            {createMutation.isPending ? 'Creating…' : 'Create Saved Job'}
          </button>
        </form>
      </details>

      {errorMessage && <p className="error-text">{errorMessage}</p>}
    </>
  );
}
