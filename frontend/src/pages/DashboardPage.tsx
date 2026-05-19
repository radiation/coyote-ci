import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { listBuilds, listProjects, listQueue } from "../api";
import { BuildActivityList } from "../components/BuildActivityList";
import { PageHeader } from "../components/PageHeader";
import { ProjectList } from "../components/ProjectList";
import { SummaryCard } from "../components/SummaryCard";
import type { Build } from "../types/build";

const DASHBOARD_RECENT_LIMIT = 6;
const DASHBOARD_PROJECT_LIMIT = 6;

function sortByNewest(builds: Build[]): Build[] {
  return [...builds].sort((left, right) => {
    const leftTime = Date.parse(
      left.finished_at ?? left.started_at ?? left.queued_at ?? left.created_at,
    );
    const rightTime = Date.parse(
      right.finished_at ??
        right.started_at ??
        right.queued_at ??
        right.created_at,
    );

    return rightTime - leftTime;
  });
}

export function DashboardPage() {
  const {
    data: projects,
    isLoading: projectsLoading,
    error: projectsError,
  } = useQuery({
    queryKey: ["projects"],
    queryFn: listProjects,
  });

  const {
    data: queueEntries,
    isLoading: queueLoading,
    error: queueError,
  } = useQuery({
    queryKey: ["dashboardQueue"],
    queryFn: () => listQueue(),
  });

  const {
    data: builds,
    isLoading: buildsLoading,
    error: buildsError,
  } = useQuery({
    queryKey: ["dashboardBuilds"],
    queryFn: () => listBuilds(),
  });

  const recentBuilds = sortByNewest(builds ?? []).slice(
    0,
    DASHBOARD_RECENT_LIMIT,
  );
  const failedBuilds = (builds ?? []).filter(
    (build) => build.status === "failed",
  );
  const visibleProjects = (projects ?? []).slice(0, DASHBOARD_PROJECT_LIMIT);

  return (
    <div className="page-content page-dashboard">
      <PageHeader
        eyebrow="Dashboard"
        title="Where should I look right now?"
        description="Projects are the main entry point. Queue activity and recent builds surface the work that needs attention next."
        actions={
          <>
            <Link className="secondary-button" to="/projects">
              View all projects
            </Link>
            <Link className="action-link" to="/jobs/new">
              Create job
            </Link>
          </>
        }
      />

      <section
        className="dashboard-summary-grid"
        aria-label="Dashboard summary"
      >
        <SummaryCard
          title="Accessible projects"
          value={projectsLoading ? "Loading…" : String(projects?.length ?? 0)}
          description={
            projectsError
              ? `Unable to load projects: ${String(projectsError)}`
              : "Projects group jobs, builds, and artifact history."
          }
        />
        <SummaryCard
          title="Queued or running"
          value={queueLoading ? "Loading…" : String(queueEntries?.length ?? 0)}
          tone={queueEntries && queueEntries.length > 0 ? "warning" : "info"}
          description={
            queueError
              ? `Unable to load queue: ${String(queueError)}`
              : "Current work in dispatch or execution."
          }
          footer={<Link to="/queue">Open queue</Link>}
        />
        <SummaryCard
          title="Recent failures"
          value={buildsLoading ? "Loading…" : String(failedBuilds.length)}
          tone={failedBuilds.length > 0 ? "danger" : "success"}
          description={
            buildsError
              ? `Unable to load recent builds: ${String(buildsError)}`
              : failedBuilds.length > 0
                ? "Recent builds that need attention."
                : "No recent failed builds in the current result set."
          }
          footer={<Link to="/builds">Open builds</Link>}
        />
      </section>

      <div className="dashboard-layout-grid">
        <section className="panel dashboard-panel">
          <div className="dashboard-panel-header">
            <div>
              <h3>Projects</h3>
              <p className="subtle-text">Projects you can access right now.</p>
            </div>
          </div>
          {projectsLoading ? <p>Loading projects…</p> : null}
          {projectsError ? (
            <div className="empty-state">
              <p className="error-text">
                Failed to load projects: {String(projectsError)}
              </p>
            </div>
          ) : null}
          {!projectsLoading &&
          !projectsError &&
          (!projects || projects.length === 0) ? (
            <div className="empty-state">
              <p className="empty">No projects available yet.</p>
              <p className="subtle-text">
                Create a project before grouping jobs and durable build history.
              </p>
            </div>
          ) : null}
          {!projectsLoading &&
          !projectsError &&
          projects &&
          projects.length > 0 ? (
            <ProjectList projects={visibleProjects} />
          ) : null}
        </section>

        <div className="dashboard-activity-column">
          {queueLoading ? (
            <section className="panel dashboard-panel">
              <div className="dashboard-panel-header">
                <h3>Queue activity</h3>
              </div>
              <p>Loading queue…</p>
            </section>
          ) : queueError ? (
            <section className="panel dashboard-panel">
              <div className="dashboard-panel-header">
                <h3>Queue activity</h3>
              </div>
              <p className="error-text">
                Failed to load queue: {String(queueError)}
              </p>
            </section>
          ) : (
            <BuildActivityList
              title="Queue activity"
              items={(queueEntries ?? [])
                .slice(0, DASHBOARD_RECENT_LIMIT)
                .map((entry) => ({
                  kind: "queue" as const,
                  entry,
                }))}
              emptyMessage="No queued or running builds right now."
            />
          )}

          {buildsLoading ? (
            <section className="panel dashboard-panel">
              <div className="dashboard-panel-header">
                <h3>Recent builds</h3>
              </div>
              <p>Loading recent builds…</p>
            </section>
          ) : buildsError ? (
            <section className="panel dashboard-panel">
              <div className="dashboard-panel-header">
                <h3>Recent builds</h3>
              </div>
              <p className="error-text">
                Failed to load builds: {String(buildsError)}
              </p>
            </section>
          ) : (
            <BuildActivityList
              title="Recent builds"
              items={recentBuilds.map((build) => ({
                kind: "build" as const,
                build,
              }))}
              emptyMessage="No recent builds yet."
            />
          )}
        </div>
      </div>
    </div>
  );
}
