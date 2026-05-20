import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { listBuilds, listProjects, listQueue } from "../api";
import { PageHeader } from "../components/PageHeader";
import { ProjectList } from "../components/ProjectList";
import { BuildActivityRail } from "../components/ScopedBuildActivityPanels";
import { SummaryCard } from "../components/SummaryCard";

const DASHBOARD_RECENT_LIMIT = 6;
const DASHBOARD_PROJECT_LIMIT = 6;

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
    data: builds,
    isLoading: buildsLoading,
    error: buildsError,
  } = useQuery({
    queryKey: ["activity", "recent", "global", DASHBOARD_RECENT_LIMIT],
    queryFn: () => listBuilds({ limit: DASHBOARD_RECENT_LIMIT }),
  });

  const {
    data: queueEntries,
    isLoading: queueLoading,
    error: queueError,
  } = useQuery({
    queryKey: ["activity", "queue", "global", DASHBOARD_RECENT_LIMIT],
    queryFn: () => listQueue(),
  });

  const failedBuilds = (builds ?? []).filter(
    (build) => build.status === "failed",
  );
  const visibleProjects = (projects ?? []).slice(0, DASHBOARD_PROJECT_LIMIT);

  return (
    <div className="page-content page-dashboard">
      <PageHeader
        title="Dashboard"
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
          title="Projects"
          value={projectsLoading ? "Loading…" : String(projects?.length ?? 0)}
          description={
            projectsError
              ? `Unable to load projects: ${String(projectsError)}`
              : undefined
          }
        />
        <SummaryCard
          title="Queue"
          value={queueLoading ? "Loading…" : String(queueEntries?.length ?? 0)}
          tone={queueEntries && queueEntries.length > 0 ? "warning" : "info"}
          description={
            queueError
              ? `Unable to load queue: ${String(queueError)}`
              : undefined
          }
          footer={<Link to="/queue">Open queue</Link>}
        />
        <SummaryCard
          title="Failures"
          value={buildsLoading ? "Loading…" : String(failedBuilds.length)}
          tone={failedBuilds.length > 0 ? "danger" : "success"}
          description={
            buildsError
              ? `Unable to load recent builds: ${String(buildsError)}`
              : failedBuilds.length > 0
                ? undefined
                : "No recent failures."
          }
          footer={<Link to="/builds">Open builds</Link>}
        />
      </section>

      <div className="dashboard-layout-grid">
        <section className="panel dashboard-panel">
          <div className="dashboard-panel-header">
            <div>
              <h3>Projects</h3>
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
            </div>
          ) : null}
          {!projectsLoading &&
          !projectsError &&
          projects &&
          projects.length > 0 ? (
            <ProjectList projects={visibleProjects} />
          ) : null}
        </section>

        <BuildActivityRail
          scope={{ type: "global" }}
          limit={DASHBOARD_RECENT_LIMIT}
        />
      </div>
    </div>
  );
}
