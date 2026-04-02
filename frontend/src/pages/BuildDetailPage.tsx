import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useParams } from 'react-router-dom';
import { useNavigate } from 'react-router-dom';
import { artifactDownloadURL, getBuild, getBuildArtifacts, getBuildSteps, rerunBuildFromStep, retryFailedJob } from '../api';
import { StatusBadge } from '../components/StatusBadge';
import { StepList } from '../components/StepList';
import type { Build } from '../types';
import { FAST_POLL_INTERVAL, SLOW_POLL_INTERVAL, isActiveBuild } from '../utils/build';
import { formatTime } from '../utils/time';

export function BuildDetailPage() {
  const { id } = useParams<{ id: string }>();
  const queryClient = useQueryClient();
  const navigate = useNavigate();

  const {
    data: build,
    isLoading: buildLoading,
    error: buildError,
    dataUpdatedAt: buildUpdatedAt,
  } = useQuery({
    queryKey: ['build', id],
    queryFn: () => getBuild(id!),
    enabled: !!id,
    refetchInterval: (query) => {
      const nextBuild = query.state.data as Build | undefined;
      return isActiveBuild(nextBuild?.status) ? FAST_POLL_INTERVAL : SLOW_POLL_INTERVAL;
    },
  });

  const retryMutation = useMutation({
    mutationFn: (jobID: string) => retryFailedJob(jobID),
    onSuccess: async (result) => {
      await queryClient.invalidateQueries({ queryKey: ['builds'] });
      await queryClient.invalidateQueries({ queryKey: ['build', id] });
      await queryClient.invalidateQueries({ queryKey: ['buildSteps', id] });
      navigate(`/builds/${result.build.id}`);
    },
  });

  const rerunMutation = useMutation({
    mutationFn: (stepIndex: number) => rerunBuildFromStep(id!, stepIndex),
    onSuccess: async (nextBuild) => {
      await queryClient.invalidateQueries({ queryKey: ['builds'] });
      await queryClient.invalidateQueries({ queryKey: ['build', id] });
      await queryClient.invalidateQueries({ queryKey: ['buildSteps', id] });
      navigate(`/builds/${nextBuild.id}`);
    },
  });

  const {
    data: steps,
    isLoading: stepsLoading,
    error: stepsError,
  } = useQuery({
    queryKey: ['buildSteps', id],
    queryFn: () => getBuildSteps(id!),
    enabled: !!id,
    refetchInterval: isActiveBuild(build?.status) ? FAST_POLL_INTERVAL : SLOW_POLL_INTERVAL,
  });

  const {
    data: artifacts,
    isLoading: artifactsLoading,
    error: artifactsError,
  } = useQuery({
    queryKey: ['buildArtifacts', id],
    queryFn: () => getBuildArtifacts(id!),
    enabled: !!id,
    refetchInterval: isActiveBuild(build?.status) ? FAST_POLL_INTERVAL : SLOW_POLL_INTERVAL,
  });

  if (buildLoading) return <p>Loading build…</p>;
  if (buildError) return <p className="error-text">Failed to load build: {String(buildError)}</p>;
  if (!build) return <p className="error-text">Build not found.</p>;

  const canRerunFromStep = build.status === 'success' || build.status === 'failed';
  const lineageSummary = build.rerun_of_build_id
    ? `Rerun of build ${build.rerun_of_build_id.slice(0, 8)}...`
    : 'Original attempt';
  const rerunFromSummary = build.rerun_from_step_index !== null && build.rerun_from_step_index !== undefined
    ? `From step ${build.rerun_from_step_index}`
    : 'From first step';

  return (
    <>
      <Link to="/builds">← Back to builds</Link>

      <h2>Build {build.id.slice(0, 8)}…</h2>
      <div className="detail-summary">
        <span><strong>Project:</strong> {build.project_id}</span>
        <span><strong>Status:</strong> <StatusBadge status={build.status} /></span>
        <span><strong>Attempt:</strong> {build.attempt_number ?? 1}</span>
        <span><strong>Lineage:</strong> {lineageSummary}</span>
        {build.rerun_of_build_id && <span><strong>Rerun:</strong> {rerunFromSummary}</span>}
        {build.commit_sha && <span><strong>Commit:</strong> {build.commit_sha.slice(0, 12)}</span>}
        <span><strong>Execution Basis:</strong> {build.execution_basis ?? 'persisted source/spec'}</span>
        <span><strong>Output Policy:</strong> {build.output_reuse_policy ?? 'explicit declared outputs only'}</span>
        <span><strong>Current Step:</strong> {build.current_step_index}</span>
      </div>
      <p className="subtle-text">Last updated: {buildUpdatedAt > 0 ? formatTime(new Date(buildUpdatedAt).toISOString()) : '—'}</p>

      <div className="detail-grid">
        <div><strong>ID</strong><span>{build.id}</span></div>
        <div><strong>Project</strong><span>{build.project_id}</span></div>
        <div><strong>Status</strong><span><StatusBadge status={build.status} /></span></div>
        <div><strong>Current Step</strong><span>{build.current_step_index}</span></div>
        <div><strong>Attempt Number</strong><span>{build.attempt_number ?? 1}</span></div>
        <div><strong>Rerun Of Build</strong><span>{build.rerun_of_build_id ?? '—'}</span></div>
        <div><strong>Rerun From Step</strong><span>{build.rerun_from_step_index ?? '—'}</span></div>
        <div><strong>Execution Basis</strong><span>{build.execution_basis ?? 'persisted source/spec'}</span></div>
        <div><strong>Output Reuse Policy</strong><span>{build.output_reuse_policy ?? 'explicit declared outputs only'}</span></div>
        <div><strong>Created</strong><span>{formatTime(build.created_at)}</span></div>
        <div><strong>Queued</strong><span>{formatTime(build.queued_at)}</span></div>
        <div><strong>Started</strong><span>{formatTime(build.started_at)}</span></div>
        <div><strong>Finished</strong><span>{formatTime(build.finished_at)}</span></div>
        {build.pipeline_source && <div><strong>Pipeline Source</strong><span>{build.pipeline_source}</span></div>}
        {build.pipeline_path && <div><strong>Pipeline Path</strong><span>{build.pipeline_path}</span></div>}
        {build.repo_url && <div><strong>Repository</strong><span>{build.repo_url}</span></div>}
        {build.ref && <div><strong>Ref</strong><span>{build.ref}</span></div>}
        {build.commit_sha && <div><strong>Commit</strong><span>{build.commit_sha}</span></div>}
        <div><strong>Error</strong><span className="error-text">{build.error_message ?? '—'}</span></div>
      </div>

      <h3>Steps</h3>
      {stepsLoading && <p>Loading steps…</p>}
      {stepsError && <p className="error-text">Failed to load steps: {String(stepsError)}</p>}
      {steps && (
        <StepList
          buildID={build.id}
          steps={steps}
          canRerunFromStep={canRerunFromStep}
          onRetryFailedJob={async (jobID) => {
            await retryMutation.mutateAsync(jobID);
          }}
          onRerunFromStep={async (stepIndex) => {
            await rerunMutation.mutateAsync(stepIndex);
          }}
          retryingJobID={retryMutation.isPending ? retryMutation.variables ?? null : null}
          rerunningStepIndex={rerunMutation.isPending ? rerunMutation.variables ?? null : null}
          actionError={
            retryMutation.error ? String(retryMutation.error)
              : rerunMutation.error ? String(rerunMutation.error)
                : null
          }
        />
      )}

      <h3>Artifacts</h3>
      {artifactsLoading && <p>Loading artifacts…</p>}
      {artifactsError && <p className="error-text">Failed to load artifacts: {String(artifactsError)}</p>}
      {!artifactsLoading && !artifactsError && artifacts && artifacts.length === 0 && (
        <p className="subtle-text">No artifacts were collected for this build.</p>
      )}
      {!artifactsLoading && artifacts && artifacts.length > 0 && (
        <table className="table artifacts-table">
          <thead>
            <tr>
              <th>Path</th>
              <th>Size</th>
              <th>Created</th>
              <th>Download</th>
            </tr>
          </thead>
          <tbody>
            {artifacts.map((item) => (
              <tr key={item.id}>
                <td>{item.path}</td>
                <td>{item.size_bytes} bytes</td>
                <td>{formatTime(item.created_at)}</td>
                <td>
                  <a href={artifactDownloadURL(item.download_url_path)}>Download</a>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </>
  );
}
