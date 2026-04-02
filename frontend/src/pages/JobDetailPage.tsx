import { useState, type FormEvent } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { getJob, runJob, updateJob } from '../api';
import type { Job } from '../types/job';
import { formatTime } from '../utils/time';

export function JobDetailPage() {
  const { id } = useParams<{ id: string }>();

  const {
    data: job,
    isLoading,
    error,
    dataUpdatedAt,
  } = useQuery({
    queryKey: ['job', id],
    queryFn: () => getJob(id!),
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

  return (
    <>
      <Link to="/jobs">← Back to jobs</Link>
      <h2>Job: {job.name}</h2>
      <p className="subtle-text">Last loaded: {dataUpdatedAt > 0 ? formatTime(new Date(dataUpdatedAt).toISOString()) : '—'}</p>

      <div className="detail-grid">
        <div><strong>ID</strong><span>{job.id}</span></div>
        <div><strong>Project</strong><span>{job.project_id}</span></div>
        <div><strong>Push Trigger</strong><span>{job.push_enabled ? 'Enabled' : 'Disabled'}</span></div>
        <div><strong>Push Branch</strong><span>{job.push_enabled ? (job.push_branch || 'Any branch') : '—'}</span></div>
        <div><strong>Created</strong><span>{formatTime(job.created_at)}</span></div>
        <div><strong>Updated</strong><span>{formatTime(job.updated_at)}</span></div>
      </div>

      <p className="subtle-text">Internal push events can be sent to POST /events/push with repository_url, ref, and commit_sha.</p>

      <JobDetailForm
        key={`${job.id}:${job.updated_at}`}
        job={job}
        jobID={id}
      />
    </>
  );
}

function JobDetailForm({ job, jobID }: { job: Job; jobID: string }) {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [name, setName] = useState(job.name);
  const [repositoryURL, setRepositoryURL] = useState(job.repository_url);
  const [defaultRef, setDefaultRef] = useState(job.default_ref);
  const [pushEnabled, setPushEnabled] = useState(job.push_enabled);
  const [pushBranch, setPushBranch] = useState(job.push_branch ?? '');
  const [pipelineYAML, setPipelineYAML] = useState(job.pipeline_yaml);
  const [enabled, setEnabled] = useState(job.enabled);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const [successMessage, setSuccessMessage] = useState<string | null>(null);

  const saveMutation = useMutation({
    mutationFn: (targetID: string) =>
      updateJob(targetID, {
        name: name.trim(),
        repository_url: repositoryURL.trim(),
        default_ref: defaultRef.trim(),
        push_enabled: pushEnabled,
        push_branch: pushEnabled ? pushBranch.trim() : '',
        pipeline_yaml: pipelineYAML.trim(),
        enabled,
      }),
    onMutate: () => {
      setErrorMessage(null);
      setSuccessMessage(null);
    },
    onSuccess: async (updated) => {
      setSuccessMessage('Job saved.');
      await queryClient.invalidateQueries({ queryKey: ['job', updated.id] });
      await queryClient.invalidateQueries({ queryKey: ['jobs'] });
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
      setSuccessMessage('Job run started.');
    },
    onError: (mutationError) => {
      setErrorMessage(`Failed to run job: ${String(mutationError)}`);
    },
  });

  const onSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();

    if (!name.trim() || !repositoryURL.trim() || !defaultRef.trim() || !pipelineYAML.trim()) {
      setErrorMessage('Name, repository URL, default ref, and pipeline YAML are required.');
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

        <label htmlFor="job-pipeline-yaml">Pipeline YAML</label>
        <textarea
          id="job-pipeline-yaml"
          value={pipelineYAML}
          onChange={(event) => setPipelineYAML(event.target.value)}
          rows={14}
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
            {saveMutation.isPending ? 'Saving…' : 'Save Job'}
          </button>
          <button
            type="button"
            className="secondary-button"
            onClick={() => runNowMutation.mutate(jobID)}
            disabled={isSubmitting}
          >
            {runNowMutation.isPending ? 'Running…' : 'Run Now'}
          </button>
        </div>
      </form>

      {successMessage && <p className="success-text">{successMessage}</p>}
      {errorMessage && <p className="error-text">{errorMessage}</p>}
    </>
  );
}
