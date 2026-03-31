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

  const onSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();

    const trimmedProjectID = projectID.trim();
    const trimmedName = name.trim();
    const trimmedRepositoryURL = repositoryURL.trim();
    const trimmedDefaultRef = defaultRef.trim();
    const trimmedPipelineYAML = pipelineYAML.trim();

    if (!trimmedProjectID || !trimmedName || !trimmedRepositoryURL || !trimmedDefaultRef || !trimmedPipelineYAML) {
      setErrorMessage('Project ID, name, repository URL, default ref, and pipeline YAML are required.');
      return;
    }

    createMutation.mutate({
      project_id: trimmedProjectID,
      name: trimmedName,
      repository_url: trimmedRepositoryURL,
      default_ref: trimmedDefaultRef,
      pipeline_yaml: trimmedPipelineYAML,
      enabled,
    });
  };

  return (
    <>
      <Link to="/jobs">← Back to jobs</Link>
      <h2>Create Job</h2>
      <p className="subtle-text">Define a reusable pipeline and run it manually whenever needed.</p>

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

        <label htmlFor="job-pipeline-yaml">Pipeline YAML</label>
        <textarea
          id="job-pipeline-yaml"
          value={pipelineYAML}
          onChange={(event) => setPipelineYAML(event.target.value)}
          rows={14}
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
          {createMutation.isPending ? 'Creating…' : 'Create Job'}
        </button>
      </form>

      {errorMessage && <p className="error-text">{errorMessage}</p>}
    </>
  );
}
