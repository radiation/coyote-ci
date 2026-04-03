import { useState, type FormEvent } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { Link, useNavigate } from 'react-router-dom';
import { createPipelineBuild, createRepoBuild, listJobs, runJob } from '../api';
import { formatTime } from '../utils/time';

export function JobsListPage() {
  const navigate = useNavigate();
  const [runError, setRunError] = useState<string | null>(null);
  const [invokeError, setInvokeError] = useState<string | null>(null);
  const [projectID, setProjectID] = useState('project-1');
  const [repoURL, setRepoURL] = useState('');
  const [repoRef, setRepoRef] = useState('main');
  const [repoCommitSHA, setRepoCommitSHA] = useState('');
  const [pipelinePath, setPipelinePath] = useState('');
  const [inlinePipelineYAML, setInlinePipelineYAML] = useState(`version: 1
steps:
  - name: verify
    run: echo "inline pipeline run"
`);

  const { data: jobs, isLoading, error, dataUpdatedAt } = useQuery({
    queryKey: ['jobs'],
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
      setRunError('Run succeeded but no build id was returned.');
    },
    onError: (mutationError) => {
      setRunError(`Failed to run job: ${String(mutationError)}`);
    },
  });

  const runRepoInvocationMutation = useMutation({
    mutationFn: async ({
      targetProjectID,
      targetRepoURL,
      targetRef,
      targetCommitSHA,
      targetPipelinePath,
    }: {
      targetProjectID: string;
      targetRepoURL: string;
      targetRef?: string;
      targetCommitSHA?: string;
      targetPipelinePath?: string;
    }) => {
      return createRepoBuild({
        project_id: targetProjectID,
        repo_url: targetRepoURL,
        ...(targetRef ? { ref: targetRef } : {}),
        ...(targetCommitSHA ? { commit_sha: targetCommitSHA } : {}),
        ...(targetPipelinePath ? { pipeline_path: targetPipelinePath } : {}),
      });
    },
    onMutate: () => {
      setInvokeError(null);
    },
    onSuccess: (build) => {
      navigate(`/builds/${build.id}`);
    },
    onError: (mutationError) => {
      setInvokeError(`Failed to run repo pipeline: ${String(mutationError)}`);
    },
  });

  const runInlinePipelineMutation = useMutation({
    mutationFn: async ({ targetProjectID, yaml }: { targetProjectID: string; yaml: string }) => {
      return createPipelineBuild({ project_id: targetProjectID, pipeline_yaml: yaml });
    },
    onMutate: () => {
      setInvokeError(null);
    },
    onSuccess: (build) => {
      navigate(`/builds/${build.id}`);
    },
    onError: (mutationError) => {
      setInvokeError(`Failed to run inline pipeline: ${String(mutationError)}`);
    },
  });

  const onRunRepoPipeline = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();

    const trimmedProjectID = projectID.trim();
    const trimmedRepoURL = repoURL.trim();
    const trimmedRef = repoRef.trim();
    const trimmedCommitSHA = repoCommitSHA.trim();
    const trimmedPipelinePath = pipelinePath.trim();

    if (!trimmedProjectID) {
      setInvokeError('Project ID is required.');
      return;
    }
    if (!trimmedRepoURL) {
      setInvokeError('Repository URL is required.');
      return;
    }
    if (!trimmedRef && !trimmedCommitSHA) {
      setInvokeError('Ref or Commit SHA is required.');
      return;
    }

    runRepoInvocationMutation.mutate({
      targetProjectID: trimmedProjectID,
      targetRepoURL: trimmedRepoURL,
      ...(trimmedRef ? { targetRef: trimmedRef } : {}),
      ...(trimmedCommitSHA ? { targetCommitSHA: trimmedCommitSHA } : {}),
      ...(trimmedPipelinePath ? { targetPipelinePath: trimmedPipelinePath } : {}),
    });
  };

  const onRunInlinePipeline = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();

    const trimmedProjectID = projectID.trim();
    const trimmedYAML = inlinePipelineYAML.trim();
    if (!trimmedProjectID) {
      setInvokeError('Project ID is required.');
      return;
    }
    if (!trimmedYAML) {
      setInvokeError('Inline pipeline YAML is required.');
      return;
    }

    runInlinePipelineMutation.mutate({ targetProjectID: trimmedProjectID, yaml: trimmedYAML });
  };

  const isInvoking = runRepoInvocationMutation.isPending || runInlinePipelineMutation.isPending;

  return (
    <>
      <div className="page-header-row">
        <div>
          <h2>Jobs</h2>
          <p className="subtle-text">Jobs are the primary invocation model. Builds represent execution attempts and lineage.</p>
          <p className="subtle-text">Last updated: {dataUpdatedAt > 0 ? formatTime(new Date(dataUpdatedAt).toISOString()) : '—'}</p>
        </div>
        <div className="table-actions">
          <Link className="action-link" to="/builds">View Build Attempts</Link>
          <Link className="action-link" to="/jobs/new">Create Saved Job</Link>
        </div>
      </div>

      <form className="queue-build-form" onSubmit={onRunRepoPipeline}>
        <h3>Run Job From Repo Pipeline</h3>
        <label htmlFor="invoke-project-id">Project ID</label>
        <input
          id="invoke-project-id"
          value={projectID}
          onChange={(event) => setProjectID(event.target.value)}
          disabled={isInvoking}
          placeholder="project-1"
        />

        <label htmlFor="invoke-repo-url">Repository URL</label>
        <input
          id="invoke-repo-url"
          value={repoURL}
          onChange={(event) => setRepoURL(event.target.value)}
          disabled={isInvoking}
          placeholder="https://github.com/org/repo.git"
        />

        <label htmlFor="invoke-repo-ref">Ref</label>
        <input
          id="invoke-repo-ref"
          value={repoRef}
          onChange={(event) => setRepoRef(event.target.value)}
          disabled={isInvoking}
          placeholder="main"
        />

        <label htmlFor="invoke-repo-commit-sha">Commit SHA</label>
        <input
          id="invoke-repo-commit-sha"
          value={repoCommitSHA}
          onChange={(event) => setRepoCommitSHA(event.target.value)}
          disabled={isInvoking}
          placeholder="Optional commit SHA"
        />

        <label htmlFor="invoke-pipeline-path">Pipeline YAML Path</label>
        <input
          id="invoke-pipeline-path"
          value={pipelinePath}
          onChange={(event) => setPipelinePath(event.target.value)}
          disabled={isInvoking}
          placeholder=".coyote/pipeline.yml"
        />
        <p className="subtle-text">Runs a build from repository source plus pipeline YAML path using the repo endpoint.</p>

        <button type="submit" disabled={isInvoking}>
          {runRepoInvocationMutation.isPending ? 'Starting…' : 'Run Job From Repo Pipeline'}
        </button>
      </form>

      <details>
        <summary>Advanced: Run Inline Pipeline</summary>
        <form className="queue-build-form" onSubmit={onRunInlinePipeline}>
          <label htmlFor="inline-pipeline-yaml">Inline Pipeline YAML</label>
          <textarea
            id="inline-pipeline-yaml"
            value={inlinePipelineYAML}
            onChange={(event) => setInlinePipelineYAML(event.target.value)}
            rows={8}
            disabled={isInvoking}
          />
          <p className="subtle-text">Secondary path for one-off inline YAML runs.</p>
          <button type="submit" disabled={isInvoking}>
            {runInlinePipelineMutation.isPending ? 'Starting…' : 'Run Inline Pipeline'}
          </button>
        </form>
      </details>

      {runError && <p className="error-text">{runError}</p>}
      {invokeError && <p className="error-text">{invokeError}</p>}
      {isLoading && <p>Loading jobs…</p>}
      {error && <p className="error-text">Failed to load jobs: {String(error)}</p>}

      {!isLoading && !error && jobs && jobs.length === 0 && (
        <div className="empty-state">
          <p className="empty">No jobs yet.</p>
          <p className="subtle-text">Run from a repository pipeline path above, or create a saved job for reusable triggers.</p>
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
                <td>{job.enabled ? 'Enabled' : 'Disabled'}</td>
                <td>{job.push_enabled ? (job.push_branch ? `On ${job.push_branch}` : 'Any branch') : 'Off'}</td>
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
                      {runNowMutation.isPending && runNowMutation.variables === job.id ? 'Running…' : 'Run Now'}
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
