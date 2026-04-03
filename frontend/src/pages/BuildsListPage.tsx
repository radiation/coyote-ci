import { useQuery } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import { listBuilds } from '../api';
import { StatusBadge } from '../components/StatusBadge';
import type { Build } from '../types/build';
import { FAST_POLL_INTERVAL, SLOW_POLL_INTERVAL, isActiveBuild } from '../utils/build';
import { formatTime } from '../utils/time';

export function BuildsListPage() {
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

  return (
    <>
      <h2>Builds</h2>
      <p className="subtle-text">
        Builds are created by running a <Link to="/jobs">job</Link>.
        {' '}Last updated: {dataUpdatedAt > 0 ? formatTime(new Date(dataUpdatedAt).toISOString()) : '—'}
      </p>

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
