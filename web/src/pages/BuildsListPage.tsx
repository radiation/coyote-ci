import { useQuery } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import { listBuilds } from '../api';
import { StatusBadge } from '../components/StatusBadge';
import { formatTime } from '../components/TimeDisplay';

const POLL_INTERVAL = 5000;

export function BuildsListPage() {
  const { data: builds, isLoading, error } = useQuery({
    queryKey: ['builds'],
    queryFn: listBuilds,
    refetchInterval: POLL_INTERVAL,
  });

  if (isLoading) return <p>Loading builds…</p>;
  if (error) return <p className="error-text">Failed to load builds: {String(error)}</p>;
  if (!builds || builds.length === 0) return <p className="empty">No builds yet.</p>;

  return (
    <>
      <h2>Builds</h2>
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
    </>
  );
}
