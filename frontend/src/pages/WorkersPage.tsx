import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { listWorkers } from "../api";
import { PageHeader } from "../components/PageHeader";
import { BuildIdentityLink } from "../components/BuildIdentityLink";
import { StatusBadge } from "../components/StatusBadge";
import { SummaryCard } from "../components/SummaryCard";
import type { Worker } from "../types/worker";
import { FAST_POLL_INTERVAL, SLOW_POLL_INTERVAL } from "../utils/build";
import { formatCompactTime, formatTime } from "../utils/time";

import { shouldHideStaleWorker } from "../utils/workerVisibility";

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
  const [showHiddenStaleWorkers, setShowHiddenStaleWorkers] = useState(false);
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

  const visibilityReferenceTime = dataUpdatedAt;
  const hiddenOlderStaleWorkers =
    workers?.filter((worker) =>
      shouldHideStaleWorker(worker, visibilityReferenceTime),
    ) ?? [];
  const visibleWorkers =
    showHiddenStaleWorkers || hiddenOlderStaleWorkers.length === 0
      ? (workers ?? [])
      : (workers?.filter(
          (worker) => !shouldHideStaleWorker(worker, visibilityReferenceTime),
        ) ?? []);

  const total = visibleWorkers.length;
  const idle = visibleWorkers.filter(
    (worker) => worker.status === "idle",
  ).length;
  const busy = visibleWorkers.filter(
    (worker) => worker.status === "busy",
  ).length;
  const stale = visibleWorkers.filter(
    (worker) => worker.status === "stale",
  ).length;

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

      {workers && (
        <section
          className="dashboard-summary-grid"
          aria-label="Visible workers summary"
        >
          <SummaryCard title="Visible workers" value={String(total)} />
          <SummaryCard title="Idle" value={String(idle)} tone="info" />
          <SummaryCard title="Busy" value={String(busy)} tone="warning" />
          <SummaryCard
            title="Stale"
            value={String(stale)}
            tone={stale > 0 ? "danger" : "success"}
          />
        </section>
      )}

      <section className="panel dashboard-panel">
        <div className="dashboard-panel-header">
          <div>
            <h3>Worker activity</h3>
          </div>
          {hiddenOlderStaleWorkers.length > 0 ? (
            <div className="workers-hidden-notice">
              <span className="subtle-text">
                {hiddenOlderStaleWorkers.length} older stale{" "}
                {hiddenOlderStaleWorkers.length === 1 ? "worker" : "workers"}{" "}
                {showHiddenStaleWorkers ? "shown" : "hidden"}
              </span>
              <button
                type="button"
                className="secondary-button workers-hidden-toggle"
                aria-expanded={showHiddenStaleWorkers}
                onClick={() => {
                  setShowHiddenStaleWorkers((current) => !current);
                }}
              >
                {showHiddenStaleWorkers
                  ? "Hide older stale workers"
                  : "Show hidden stale workers"}
              </button>
            </div>
          ) : null}
        </div>

        {isLoading ? <p role="status">Loading workers…</p> : null}
        {error ? (
          <div className="empty-state">
            <p className="error-text" role="alert">
              Unable to load workers.
              {workers ? " Showing last known results." : ""}
            </p>
          </div>
        ) : null}
        {!isLoading && !error && total === 0 ? (
          <div className="empty-state">
            <p className="empty">
              {hiddenOlderStaleWorkers.length
                ? "All reported workers are hidden by the stale-worker filter."
                : "No workers have reported heartbeats yet."}
            </p>
          </div>
        ) : null}

        {visibleWorkers.length > 0 ? (
          <ul className="activity-list worker-activity">
            {visibleWorkers.map((worker) => (
              <li
                key={worker.id}
                className={
                  worker.status === "stale" ? "workers-row-stale" : undefined
                }
              >
                <div className="activity-list-title-row">
                  <strong>{worker.name}</strong>
                  <StatusBadge status={worker.status} />
                  <span className="subtle-text">{worker.id}</span>
                  {worker.stale_lease ? (
                    <div className="subtle-text">Expired lease</div>
                  ) : null}
                  {worker.stale_heartbeat ? (
                    <div className="subtle-text">Heartbeat overdue</div>
                  ) : null}
                </div>
                <dl>
                  <div>
                    <dt>Current build</dt>
                    <dd>
                      {worker.current_build_id ? (
                        <BuildIdentityLink
                          id={worker.current_build_id}
                          number={worker.current_build_number}
                        />
                      ) : (
                        "—"
                      )}
                    </dd>
                  </div>
                  <div>
                    <dt>Current step</dt>
                    <dd>{currentStepLabel(worker)}</dd>
                  </div>
                  <div>
                    <dt>Project</dt>
                    <dd>
                      {worker.project_id ? (
                        <Link to={`/projects/${worker.project_id}`}>
                          {contextLabel(worker.project_name, worker.project_id)}
                        </Link>
                      ) : (
                        contextLabel(worker.project_name, null)
                      )}
                    </dd>
                  </div>
                  <div>
                    <dt>Job</dt>
                    <dd>
                      {worker.job_id ? (
                        <Link to={`/jobs/${worker.job_id}`}>
                          {contextLabel(worker.job_name, worker.job_id)}
                        </Link>
                      ) : (
                        contextLabel(worker.job_name, null)
                      )}
                    </dd>
                  </div>
                  <div>
                    <dt>Last heartbeat</dt>
                    <dd>{formatTime(worker.last_heartbeat_at)}</dd>
                  </div>
                  <div>
                    <dt>Claimed at</dt>
                    <dd>{formatTime(worker.claimed_at)}</dd>
                  </div>
                  <div>
                    <dt>Lease expires</dt>
                    <dd>{formatTime(worker.lease_expires_at)}</dd>
                  </div>
                </dl>
              </li>
            ))}
          </ul>
        ) : null}
      </section>
    </div>
  );
}
