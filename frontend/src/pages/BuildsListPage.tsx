import { useState, type FormEvent } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import { useNavigate } from 'react-router-dom';
import { createBuild, listBuilds, queueBuild } from '../api';
import { StatusBadge } from '../components/StatusBadge';
import { formatTime } from '../components/TimeDisplay';
import type { BuildTemplate } from '../types';

const POLL_INTERVAL = 5000;

export function BuildsListPage() {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const [projectID, setProjectID] = useState('project-1');
  const [template, setTemplate] = useState<BuildTemplate>('default');
  const [successMessage, setSuccessMessage] = useState<string | null>(null);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);

  const { data: builds, isLoading, error } = useQuery({
    queryKey: ['builds'],
    queryFn: listBuilds,
    refetchInterval: POLL_INTERVAL,
  });

  const queueBuildMutation = useMutation({
    mutationFn: async ({ targetProjectID, targetTemplate }: { targetProjectID: string; targetTemplate: BuildTemplate }) => {
      const createdBuild = await createBuild({ project_id: targetProjectID });
      return queueBuild(createdBuild.id, targetTemplate);
    },
    onMutate: () => {
      setSuccessMessage(null);
      setErrorMessage(null);
    },
    onSuccess: async (queuedBuild) => {
      await queryClient.invalidateQueries({ queryKey: ['builds'] });

      if (queuedBuild.id) {
        navigate(`/builds/${queuedBuild.id}`);
        return;
      }

      setSuccessMessage('Build queued. It should appear at the top of the builds list.');
    },
    onError: (mutationError) => {
      setErrorMessage(`Failed to queue build: ${String(mutationError)}`);
    },
  });

  const onQueueBuild = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const trimmedProjectID = projectID.trim();
    if (!trimmedProjectID) {
      setErrorMessage('Project ID is required.');
      return;
    }

    queueBuildMutation.mutate({ targetProjectID: trimmedProjectID, targetTemplate: template });
  };

  return (
    <>
      <h2>Builds</h2>

      <form className="queue-build-form" onSubmit={onQueueBuild}>
        <label htmlFor="project-id">Project ID</label>
        <input
          id="project-id"
          value={projectID}
          onChange={(event) => setProjectID(event.target.value)}
          disabled={queueBuildMutation.isPending}
          placeholder="project-1"
        />
        <label htmlFor="pipeline-template">Template</label>
        <select
          id="pipeline-template"
          value={template}
          onChange={(event) => setTemplate(event.target.value as BuildTemplate)}
          disabled={queueBuildMutation.isPending}
        >
          <option value="default">default</option>
          <option value="test">test</option>
          <option value="build">build</option>
        </select>
        <button type="submit" disabled={queueBuildMutation.isPending}>
          {queueBuildMutation.isPending ? 'Queueing…' : 'Queue Build'}
        </button>
      </form>

      {successMessage && <p className="success-text">{successMessage}</p>}
      {errorMessage && <p className="error-text">{errorMessage}</p>}

      {isLoading && <p>Loading builds…</p>}
      {error && <p className="error-text">Failed to load builds: {String(error)}</p>}
      {!isLoading && !error && (!builds || builds.length === 0) && <p className="empty">No builds yet.</p>}

      {builds && builds.length > 0 && (
      <table className="table">
        <thead>
          <tr>
            <th>ID</th>
            <th>Project</th>
            <th>Status</th>
            <th>Step</th>
            <th>Created</th>
            <th>Queued</th>
            <th>Started</th>
            <th>Finished</th>
            <th>Error</th>
          </tr>
        </thead>
        <tbody>
          {builds.map((b) => (
            <tr key={b.id}>
              <td>
                <Link to={`/builds/${b.id}`}>{b.id.slice(0, 8)}…</Link>
              </td>
              <td>{b.project_id}</td>
              <td><StatusBadge status={b.status} /></td>
              <td>{b.current_step_index}</td>
              <td>{formatTime(b.created_at)}</td>
              <td>{formatTime(b.queued_at)}</td>
              <td>{formatTime(b.started_at)}</td>
              <td>{formatTime(b.finished_at)}</td>
              <td className="error-text">{b.error_message ?? ''}</td>
            </tr>
          ))}
        </tbody>
      </table>
      )}
    </>
  );
}
