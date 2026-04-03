import { useState, type FormEvent } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import { useNavigate } from 'react-router-dom';
import { createBuild, createPipelineBuild, createRepoBuild, listBuilds, queueBuild } from '../api';
import { StatusBadge } from '../components/StatusBadge';
import type { Build, BuildTemplate, QueueBuildStepInput } from '../types/build';
import { FAST_POLL_INTERVAL, SLOW_POLL_INTERVAL, isActiveBuild } from '../utils/build';
import { formatTime } from '../utils/time';

const PIPELINE_YAML_EXAMPLE = `version: 1
pipeline:
  name: my-pipeline
steps:
  - name: greet
    run: echo "Hello from pipeline"
  - name: build
    run: go build ./...
`;

export function BuildsListPage() {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const [projectID, setProjectID] = useState('project-1');
  const [template, setTemplate] = useState<BuildTemplate>('default');
  const [customCommands, setCustomCommands] = useState('');
  const [pipelineYAML, setPipelineYAML] = useState(PIPELINE_YAML_EXAMPLE);
  const [repoURL, setRepoURL] = useState('');
  const [repoRef, setRepoRef] = useState('main');
  const [repoCommitSHA, setRepoCommitSHA] = useState('');
  const [repoPipelinePath, setRepoPipelinePath] = useState('');
  const [successMessage, setSuccessMessage] = useState<string | null>(null);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);

  const { data: builds, isLoading, error, dataUpdatedAt } = useQuery({
    queryKey: ['builds'],
    queryFn: listBuilds,
    refetchInterval: (query) => {
      const nextBuilds = query.state.data as Build[] | undefined;
      if (!nextBuilds || nextBuilds.length === 0) {
        return SLOW_POLL_INTERVAL;
      }

      return nextBuilds.some((b) => isActiveBuild(b.status)) ? FAST_POLL_INTERVAL : SLOW_POLL_INTERVAL;
    },
  });

  const queueBuildMutation = useMutation({
    mutationFn: async ({
      targetProjectID,
      targetTemplate,
      customSteps,
    }: {
      targetProjectID: string;
      targetTemplate: BuildTemplate;
      customSteps?: QueueBuildStepInput[];
    }) => {
      const createdBuild = await createBuild({ project_id: targetProjectID });
      const queuedBuild = await queueBuild(createdBuild.id, targetTemplate, customSteps);
      return { createdBuildID: createdBuild.id, queuedBuildID: queuedBuild.id };
    },
    onMutate: () => {
      setSuccessMessage(null);
      setErrorMessage(null);
    },
    onSuccess: async ({ createdBuildID, queuedBuildID }) => {
      await queryClient.invalidateQueries({ queryKey: ['builds'] });

      const nextBuildID = queuedBuildID || createdBuildID;
      if (nextBuildID) {
        navigate(`/builds/${nextBuildID}`);
        return;
      }

      setSuccessMessage('Build queued. It should appear at the top of the builds list.');
    },
    onError: (mutationError) => {
      setErrorMessage(`Failed to queue build: ${String(mutationError)}`);
    },
  });

  const pipelineBuildMutation = useMutation({
    mutationFn: async ({ targetProjectID, yaml }: { targetProjectID: string; yaml: string }) => {
      return createPipelineBuild({ project_id: targetProjectID, pipeline_yaml: yaml });
    },
    onMutate: () => {
      setSuccessMessage(null);
      setErrorMessage(null);
    },
    onSuccess: async (build) => {
      await queryClient.invalidateQueries({ queryKey: ['builds'] });
      navigate(`/builds/${build.id}`);
    },
    onError: (mutationError) => {
      setErrorMessage(`Failed to create pipeline build: ${String(mutationError)}`);
    },
  });

  const repoBuildMutation = useMutation({
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
      setSuccessMessage(null);
      setErrorMessage(null);
    },
    onSuccess: async (build) => {
      await queryClient.invalidateQueries({ queryKey: ['builds'] });
      navigate(`/builds/${build.id}`);
    },
    onError: (mutationError) => {
      setErrorMessage(`Failed to create repo build: ${String(mutationError)}`);
    },
  });

  const isSubmitting = queueBuildMutation.isPending || pipelineBuildMutation.isPending || repoBuildMutation.isPending;

  const onQueueBuild = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const trimmedProjectID = projectID.trim();
    if (!trimmedProjectID) {
      setErrorMessage('Project ID is required.');
      return;
    }

    if (template === 'pipeline') {
      const trimmedYAML = pipelineYAML.trim();
      if (!trimmedYAML) {
        setErrorMessage('Pipeline YAML is required.');
        return;
      }
      pipelineBuildMutation.mutate({ targetProjectID: trimmedProjectID, yaml: trimmedYAML });
      return;
    }

    if (template === 'repo') {
      const trimmedRepoURL = repoURL.trim();
      const trimmedRef = repoRef.trim();
      const trimmedCommitSHA = repoCommitSHA.trim();
      const trimmedPipelinePath = repoPipelinePath.trim();
      if (!trimmedRepoURL) {
        setErrorMessage('Repository URL is required.');
        return;
      }
      if (!trimmedRef && !trimmedCommitSHA) {
        setErrorMessage('Either ref or commit SHA is required.');
        return;
      }

      repoBuildMutation.mutate({
        targetProjectID: trimmedProjectID,
        targetRepoURL: trimmedRepoURL,
        ...(trimmedRef ? { targetRef: trimmedRef } : {}),
        ...(trimmedCommitSHA ? { targetCommitSHA: trimmedCommitSHA } : {}),
        ...(trimmedPipelinePath ? { targetPipelinePath: trimmedPipelinePath } : {}),
      });
      return;
    }

    let customSteps: QueueBuildStepInput[] | undefined
    if (template === 'custom') {
      const commands = customCommands
        .split('\n')
        .map((line) => line.trim())
        .filter((line) => line.length > 0);

      if (commands.length === 0) {
        setErrorMessage('At least one custom command is required.');
        return;
      }

      customSteps = commands.map((command) => ({ command }));
    }

    queueBuildMutation.mutate({ targetProjectID: trimmedProjectID, targetTemplate: template, customSteps });
  };

  return (
    <>
      <h2>Build Attempts</h2>
      <p className="subtle-text">Execution attempts and lineage history for jobs and manual invocations.</p>
      <p className="subtle-text">Last updated: {dataUpdatedAt > 0 ? formatTime(new Date(dataUpdatedAt).toISOString()) : '—'}</p>

      <form className="queue-build-form" onSubmit={onQueueBuild}>
        <label htmlFor="project-id">Project ID</label>
        <input
          id="project-id"
          value={projectID}
          onChange={(event) => setProjectID(event.target.value)}
          disabled={isSubmitting}
          placeholder="project-1"
        />
        <label htmlFor="pipeline-template">Template</label>
        <select
          id="pipeline-template"
          value={template}
          onChange={(event) => setTemplate(event.target.value as BuildTemplate)}
          disabled={isSubmitting}
        >
          <option value="repo">repo build (url + ref/commit + path)</option>
          <option value="pipeline">inline pipeline build (yaml)</option>
          <option value="default">default</option>
          <option value="test">test</option>
          <option value="build">build</option>
          <option value="custom">custom</option>
        </select>
        {template === 'custom' && (
          <div className="queue-build-custom-input">
            <label htmlFor="custom-commands">Commands</label>
            <textarea
              id="custom-commands"
              value={customCommands}
              onChange={(event) => setCustomCommands(event.target.value)}
              disabled={isSubmitting}
              placeholder={'echo ok && exit 0\necho fail && exit 1'}
              rows={4}
            />
            <p className="subtle-text">One command per line. Each line becomes a step and runs via sh -c.</p>
          </div>
        )}
        {template === 'pipeline' && (
          <div className="queue-build-custom-input">
            <label htmlFor="pipeline-yaml">Pipeline YAML</label>
            <textarea
              id="pipeline-yaml"
              value={pipelineYAML}
              onChange={(event) => setPipelineYAML(event.target.value)}
              disabled={isSubmitting}
              rows={10}
            />
            <p className="subtle-text">Paste a Coyote CI pipeline definition. The backend will validate it.</p>
          </div>
        )}
        {template === 'repo' && (
          <div className="queue-build-custom-input">
            <label htmlFor="repo-url">Repository URL</label>
            <input
              id="repo-url"
              value={repoURL}
              onChange={(event) => setRepoURL(event.target.value)}
              disabled={isSubmitting}
              placeholder="https://github.com/org/repo.git"
            />
            <label htmlFor="repo-ref">Ref</label>
            <input
              id="repo-ref"
              value={repoRef}
              onChange={(event) => setRepoRef(event.target.value)}
              disabled={isSubmitting}
              placeholder="main"
            />
            <label htmlFor="repo-commit-sha">Commit SHA</label>
            <input
              id="repo-commit-sha"
              value={repoCommitSHA}
              onChange={(event) => setRepoCommitSHA(event.target.value)}
              disabled={isSubmitting}
              placeholder="Optional commit SHA override"
            />
            <details>
              <summary>Advanced</summary>
              <div className="queue-build-custom-input">
                <label htmlFor="repo-pipeline-path">Pipeline path</label>
                <input
                  id="repo-pipeline-path"
                  value={repoPipelinePath}
                  onChange={(event) => setRepoPipelinePath(event.target.value)}
                  disabled={isSubmitting}
                  placeholder=".coyote/pipeline.yml"
                />
                <p className="subtle-text">Optional path to a pipeline file inside the repository. If omitted, the default pipeline path is used.</p>
              </div>
            </details>
            <p className="subtle-text">Repo builds call the backend repo endpoint and load pipeline YAML from the repository path above.</p>
            <p className="subtle-text">Public HTTPS repositories are the current expected path unless credentials are separately configured.</p>
          </div>
        )}
        <button type="submit" disabled={isSubmitting}>
          {isSubmitting ? 'Submitting…' : template === 'pipeline' ? 'Create Inline Pipeline Build' : template === 'repo' ? 'Create Repo Build' : 'Queue Build'}
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
              <td className="error-text">{b.error_message ?? '—'}</td>
            </tr>
          ))}
        </tbody>
      </table>
      )}
    </>
  );
}
