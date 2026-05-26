import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { listWorkers } from "../api";
import { PageHeader } from "../components/PageHeader";
import { StatusBadge } from "../components/StatusBadge";
import { SummaryCard } from "../components/SummaryCard";
import type { Worker } from "../types/worker";
import { FAST_POLL_INTERVAL, SLOW_POLL_INTERVAL } from "../utils/build";
import { formatCompactTime, formatTime } from "../utils/time";

function buildLabel(worker: Worker): string {
  if (typeof worker.current_build_number === "number") {
    return `Build #${worker.current_build_number}`;
  }
  if (worker.current_build_id) {
    return `Build ${worker.current_build_id.slice(0, 8)}`;
  }
  return "—";
}

function contextLabel(
  primary: string | null | undefined,
  secondary: string | null | undefined,
): string {
  return primary?.trim() || secondary?.trim() || "—";
}

function currentStepLabel(worker: Worker): string {
  if (
    !worker.current_step_name &&
    typeof worker.current_step_index !== "number"
  ) {
    return "—";
  }

  const parts = [] as string[];
  if (typeof worker.current_step_index === "number") {
    parts.push(`Step ${worker.current_step_index + 1}`);
  }
  if (worker.current_step_name) {
    parts.push(worker.current_step_name);
  }
  return parts.join(" · ");
}

export function WorkersPage() {
  const [manualRefreshPending, setManualRefreshPending] = useState(false);
  const {
    data: workers,
    isLoading,
    isFetching,
    dataUpdatedAt,
    error,
    refetch,
  } = useQuery({
    queryKey: ["workers"],
    queryFn: listWorkers,
    refetchInterval: (query) => {
      const nextWorkers = query.state.data as Worker[] | undefined;
      if (!nextWorkers || nextWorkers.length === 0) {
        return SLOW_POLL_INTERVAL;
      }

      return nextWorkers.some(
        (worker) => worker.status === "busy" || worker.status === "stale",
      )
        ? FAST_POLL_INTERVAL
        : SLOW_POLL_INTERVAL;
    },
  });

  const isRefreshing = manualRefreshPending || (isFetching && !isLoading);
  const lastRefreshed =
    dataUpdatedAt > 0
      ? formatCompactTime(new Date(dataUpdatedAt).toISOString())
      : null;

  async function handleRefresh() {
    setManualRefreshPending(true);
    try {
      await refetch();
    } finally {
      setManualRefreshPending(false);
    }
  }

  const total = workers?.length ?? 0;
  const idle =
    workers?.filter((worker) => worker.status === "idle").length ?? 0;
  const busy =
    workers?.filter((worker) => worker.status === "busy").length ?? 0;
  const stale =
    workers?.filter((worker) => worker.status === "stale").length ?? 0;

  return (
    <div className="page-content page-workers">
      <PageHeader
        title="Workers"
        description="Current worker heartbeats, leases, and claimed build execution."
        actions={
          <div className="operational-refresh-controls">
            <button
              type="button"
              className="secondary-button operational-refresh-button"
              onClick={() => {
                void handleRefresh();
              }}
              disabled={isLoading || isRefreshing}
            >
              {isRefreshing ? "Refreshing…" : "Refresh"}
            </button>
            <div className="subtle-text operational-refresh-meta">
              {isRefreshing
                ? "Refreshing worker state…"
                : lastRefreshed
                  ? `Last refreshed ${lastRefreshed}`
                  : "Not refreshed yet"}
            </div>
          </div>
        }
      />

      <section className="dashboard-summary-grid" aria-label="Workers summary">
        <SummaryCard
          title="Total workers"
          value={isLoading ? "Loading…" : String(total)}
        />
        <SummaryCard
          title="Idle"
          value={isLoading ? "Loading…" : String(idle)}
          tone="info"
        />
        <SummaryCard
          title="Busy"
          value={isLoading ? "Loading…" : String(busy)}
          tone="warning"
        />
        <SummaryCard
          title="Stale"
          value={isLoading ? "Loading…" : String(stale)}
          tone={stale > 0 ? "danger" : "success"}
        />
      </section>

      <section className="panel dashboard-panel">
        <div className="dashboard-panel-header">
          <div>
            <h3>Worker activity</h3>
          </div>
        </div>

        {isLoading ? <p>Loading workers…</p> : null}
        {error ? (
          <div className="empty-state">
            <p className="error-text">Unable to load workers.</p>
          </div>
        ) : null}
        {!isLoading && !error && total === 0 ? (
          <div className="empty-state">
            <p className="empty">No workers have reported heartbeats yet.</p>
          </div>
        ) : null}

        {!isLoading && !error && workers && workers.length > 0 ? (
          <table className="table workers-table">
            <thead>
              <tr>
                <th>Worker</th>
                <th>Status</th>
                <th>Current build</th>
                <th>Current step</th>
                <th>Project</th>
                <th>Job</th>
                <th>Last heartbeat</th>
                <th>Lease expires</th>
                <th>Claimed at</th>
              </tr>
            </thead>
            <tbody>
              {workers.map((worker) => (
                <tr
                  key={worker.id}
                  className={
                    worker.status === "stale" ? "workers-row-stale" : undefined
                  }
                >
                  <td>
                    <strong>{worker.name}</strong>
                    <div className="subtle-text">{worker.id}</div>
                  </td>
                  <td>
                    <StatusBadge status={worker.status} />
                    {worker.stale_lease ? (
                      <div className="subtle-text">Expired lease</div>
                    ) : null}
                    {worker.stale_heartbeat ? (
                      <div className="subtle-text">Heartbeat overdue</div>
                    ) : null}
                  </td>
                  <td>
                    {worker.current_build_id ? (
                      <Link to={`/builds/${worker.current_build_id}`}>
                        {buildLabel(worker)}
                      </Link>
                    ) : (
                      "—"
                    )}
                  </td>
                  <td>{currentStepLabel(worker)}</td>
                  <td>
                    {contextLabel(worker.project_name, worker.project_id)}
                  </td>
                  <td>{contextLabel(worker.job_name, worker.job_id)}</td>
                  <td>{formatTime(worker.last_heartbeat_at)}</td>
                  <td>{formatTime(worker.lease_expires_at)}</td>
                  <td>{formatTime(worker.claimed_at)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        ) : null}
      </section>
    </div>
  );
}
