import { useQuery } from '@tanstack/react-query';
import { Link, useParams } from 'react-router-dom';
import { artifactDownloadURL, getBuild, getBuildArtifacts, getBuildSteps } from '../api';
import { StatusBadge } from '../components/StatusBadge';
import { StepList } from '../components/StepList';
import type { Build } from '../types';
import { FAST_POLL_INTERVAL, SLOW_POLL_INTERVAL, isActiveBuild } from '../utils/build';
import { formatTime } from '../utils/time';

export function BuildDetailPage() {
  const { id } = useParams<{ id: string }>();

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

  return (
    <>
      <Link to="/builds">← Back to builds</Link>
      {build.job_id && (
        <> · <Link to={`/jobs/${build.job_id}`}>← Back to job</Link></>
      )}

      <h2>Build {build.id.slice(0, 8)}…</h2>
      <div className="detail-summary">
        <span><strong>Project:</strong> {build.project_id}</span>
        <span><strong>Status:</strong> <StatusBadge status={build.status} /></span>
        <span><strong>Current Step:</strong> {build.current_step_index}</span>
      </div>
      <p className="subtle-text">Last updated: {buildUpdatedAt > 0 ? formatTime(new Date(buildUpdatedAt).toISOString()) : '—'}</p>

      <div className="detail-grid">
        <div><strong>ID</strong><span>{build.id}</span></div>
        <div><strong>Project</strong><span>{build.project_id}</span></div>
        <div><strong>Status</strong><span><StatusBadge status={build.status} /></span></div>
        <div><strong>Current Step</strong><span>{build.current_step_index}</span></div>
        <div><strong>Created</strong><span>{formatTime(build.created_at)}</span></div>
        <div><strong>Queued</strong><span>{formatTime(build.queued_at)}</span></div>
        <div><strong>Started</strong><span>{formatTime(build.started_at)}</span></div>
        <div><strong>Finished</strong><span>{formatTime(build.finished_at)}</span></div>
        {build.pipeline_source && <div><strong>Pipeline Source</strong><span>{build.pipeline_source}</span></div>}
        {build.pipeline_path && <div><strong>Pipeline Path</strong><span>{build.pipeline_path}</span></div>}
        <div><strong>Error</strong><span className="error-text">{build.error_message ?? '—'}</span></div>
      </div>

      <h3>Steps</h3>
      {stepsLoading && <p>Loading steps…</p>}
      {stepsError && <p className="error-text">Failed to load steps: {String(stepsError)}</p>}
      {steps && <StepList buildID={build.id} steps={steps} />}

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
