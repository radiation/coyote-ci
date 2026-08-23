import { useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, Navigate, useParams } from "react-router-dom";
import {
  deleteProjectMember,
  formatAPIErrorMessage,
  getProject,
  getPublicProject,
  isAPIErrorStatus,
  listPublicBuilds,
  listJobsByProject,
  listProjectMembers,
  listUsers,
  updateProjectMember,
  upsertProjectMember,
} from "../api";
import { useAuth } from "../auth-context";
import { BuildActivityRail } from "../components/ScopedBuildActivityPanels";
import type { ProjectMemberRole } from "../types/identity";
import { StatusBadge } from "../components/StatusBadge";
import { formatTime } from "../utils/time";
import type { PublicBuild } from "../types/public";
import {
  FAST_POLL_INTERVAL,
  isActiveBuild,
  SLOW_POLL_INTERVAL,
} from "../utils/build";

export function ProjectDetailPage() {
  const { authStatus } = useAuth();

  return authStatus === "unauthenticated" ? (
    <PublicProjectDetailPage />
  ) : (
    <AuthenticatedProjectRoute />
  );
}

function AuthenticatedProjectRoute() {
  const { id: projectIDOrSlug } = useParams<{ id: string }>();
  const {
    data: authenticatedProject,
    isLoading: authenticatedProjectLoading,
    error: authenticatedProjectError,
  } = useQuery({
    queryKey: ["project", projectIDOrSlug],
    queryFn: () => getProject(projectIDOrSlug!),
    enabled: Boolean(projectIDOrSlug),
    retry: false,
  });
  const {
    data: publicProject,
    isLoading: publicProjectLoading,
    error: publicProjectError,
  } = useQuery({
    queryKey: ["publicProject", projectIDOrSlug],
    queryFn: () => getPublicProject(projectIDOrSlug!),
    enabled: isAPIErrorStatus(authenticatedProjectError, 404),
    retry: false,
  });

  if (authenticatedProjectLoading) return <p>Loading project…</p>;
  if (authenticatedProject) return <AuthenticatedProjectDetailPage />;
  if (
    authenticatedProjectError &&
    !isAPIErrorStatus(authenticatedProjectError, 404)
  ) {
    return (
      <p className="error-text">
        Failed to load project: {String(authenticatedProjectError)}
      </p>
    );
  }
  if (publicProjectLoading) return <p>Loading project…</p>;
  if (publicProject) {
    return <Navigate to={`/projects/${publicProject.id}`} replace />;
  }
  if (publicProjectError && !isAPIErrorStatus(publicProjectError, 404)) {
    return (
      <p className="error-text">
        Failed to resolve public project: {String(publicProjectError)}
      </p>
    );
  }

  return <AuthenticatedProjectDetailPage />;
}

function PublicProjectDetailPage() {
  const { id: slug } = useParams<{ id: string }>();
  const {
    data: project,
    isLoading: projectLoading,
    error: projectError,
  } = useQuery({
    queryKey: ["publicProject", slug],
    queryFn: () => getPublicProject(slug!),
    enabled: Boolean(slug),
  });
  const {
    data: builds,
    isLoading: buildsLoading,
    error: buildsError,
  } = useQuery({
    queryKey: ["publicBuilds", slug],
    queryFn: () => listPublicBuilds(slug!),
    enabled: Boolean(slug && project),
    refetchInterval: (query) => {
      const nextBuilds = query.state.data as PublicBuild[] | undefined;
      return nextBuilds?.some((build) => isActiveBuild(build.status))
        ? FAST_POLL_INTERVAL
        : SLOW_POLL_INTERVAL;
    },
  });

  if (projectLoading) return <p>Loading project…</p>;
  if (projectError && isAPIErrorStatus(projectError, 404)) {
    return <p className="error-text">Project not found.</p>;
  }
  if (projectError) {
    return (
      <p className="error-text">
        Failed to load project: {String(projectError)}
      </p>
    );
  }
  if (!project) return <p className="error-text">Project not found.</p>;

  return (
    <>
      <Link to="/projects">← Back to projects</Link>
      <div className="page-header-row">
        <div className="page-header-copy">
          <h2>{project.name}</h2>
          <p className="subtle-text">
            {project.description || "No description."}
          </p>
        </div>
      </div>
      <section className="detail-panel" aria-label="Public build history">
        <h3>Build History</h3>
        {buildsLoading && <p>Loading builds…</p>}
        {buildsError && isAPIErrorStatus(buildsError, 404) && (
          <p className="error-text">Project not found.</p>
        )}
        {buildsError && !isAPIErrorStatus(buildsError, 404) && (
          <p className="error-text">
            Failed to load builds: {String(buildsError)}
          </p>
        )}
        {!buildsLoading && !buildsError && builds?.length === 0 && (
          <p className="empty">No builds yet.</p>
        )}
        {builds && builds.length > 0 && (
          <table className="table">
            <thead>
              <tr>
                <th>Build</th>
                <th>Job</th>
                <th>Status</th>
                <th>Attempt</th>
                <th>Created</th>
                <th>Completed</th>
              </tr>
            </thead>
            <tbody>
              {builds.map((build) => (
                <tr key={build.id}>
                  <td>
                    <Link to={`/projects/${project.slug}/builds/${build.id}`}>
                      #{build.number}
                    </Link>
                  </td>
                  <td>{build.job_name || "—"}</td>
                  <td>
                    <StatusBadge status={build.status} />
                  </td>
                  <td>{build.attempt}</td>
                  <td>{formatTime(build.created_at)}</td>
                  <td>{formatTime(build.completed_at)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>
    </>
  );
}

function AuthenticatedProjectDetailPage() {
  const { id } = useParams<{ id: string }>();
  const queryClient = useQueryClient();
  const [memberUserID, setMemberUserID] = useState("");
  const [selectedRole, setSelectedRole] = useState<ProjectMemberRole>("viewer");
  const [memberError, setMemberError] = useState<string | null>(null);

  const {
    data: project,
    isLoading,
    error,
  } = useQuery({
    queryKey: ["project", id],
    queryFn: () => getProject(id!),
    enabled: Boolean(id),
  });

  const {
    data: jobs,
    isLoading: jobsLoading,
    error: jobsError,
  } = useQuery({
    queryKey: ["projectJobs", id],
    queryFn: () => listJobsByProject(id!),
    enabled: Boolean(id),
  });

  const {
    data: members,
    isLoading: membersLoading,
    error: membersError,
  } = useQuery({
    queryKey: ["projectMembers", id],
    queryFn: () => listProjectMembers(id!),
    enabled: Boolean(id),
  });

  const { data: users } = useQuery({
    queryKey: ["users"],
    queryFn: listUsers,
  });

  const addMemberMutation = useMutation({
    mutationFn: () =>
      upsertProjectMember(id!, memberUserID.trim(), selectedRole),
    onMutate: () => setMemberError(null),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["projectMembers", id] });
      setMemberUserID("");
      setSelectedRole("viewer");
    },
    onError: (mutationError) =>
      setMemberError(
        formatAPIErrorMessage(
          mutationError,
          "You do not have permission to manage project members.",
        ),
      ),
  });

  const updateMemberMutation = useMutation({
    mutationFn: ({
      userID,
      role,
    }: {
      userID: string;
      role: ProjectMemberRole;
    }) => updateProjectMember(id!, userID, role),
    onMutate: () => setMemberError(null),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["projectMembers", id] });
    },
    onError: (mutationError) =>
      setMemberError(
        formatAPIErrorMessage(
          mutationError,
          "You do not have permission to manage project members.",
        ),
      ),
  });

  const deleteMemberMutation = useMutation({
    mutationFn: (userID: string) => deleteProjectMember(id!, userID),
    onMutate: () => setMemberError(null),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["projectMembers", id] });
    },
    onError: (mutationError) =>
      setMemberError(
        formatAPIErrorMessage(
          mutationError,
          "You do not have permission to manage project members.",
        ),
      ),
  });

  const onAddMember = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!memberUserID.trim()) {
      setMemberError("User ID is required.");
      return;
    }
    addMemberMutation.mutate();
  };

  if (isLoading) {
    return <p>Loading project…</p>;
  }

  if (error) {
    return (
      <p className="error-text">Failed to load project: {String(error)}</p>
    );
  }

  if (!project) {
    return <p className="error-text">Project not found.</p>;
  }

  return (
    <>
      <Link to="/projects">← Back to projects</Link>
      <div className="detail-page-with-rail">
        <div className="detail-main-column">
          <div className="page-header-row">
            <div className="page-header-copy">
              <h2>{project.name}</h2>
              <p className="subtle-text">Slug: {project.slug}</p>
            </div>
            <div className="page-header-actions">
              <Link
                className="action-link"
                to={`/jobs/new?project_id=${encodeURIComponent(project.id)}`}
              >
                Create Job
              </Link>
              <Link to={`/builds?project_id=${encodeURIComponent(project.id)}`}>
                View Builds
              </Link>
              <Link
                to={`/artifacts?project_id=${encodeURIComponent(project.id)}`}
              >
                Browse Artifacts
              </Link>
            </div>
          </div>

          <section className="detail-panel" aria-label="Project summary">
            <h3>Project Summary</h3>
            <div className="detail-grid">
              <div>
                <strong>ID</strong>
                <span>{project.id}</span>
              </div>
              <div>
                <strong>Slug</strong>
                <span>{project.slug}</span>
              </div>
              <div>
                <strong>Description</strong>
                <span>{project.description || "—"}</span>
              </div>
              <div>
                <strong>Created</strong>
                <span>{formatTime(project.created_at)}</span>
              </div>
              <div>
                <strong>Updated</strong>
                <span>{formatTime(project.updated_at)}</span>
              </div>
            </div>
          </section>

          <section className="detail-panel" aria-label="Project actions">
            <h3>Project Actions</h3>
            <div className="detail-actions-row">
              <Link
                className="action-link"
                to={`/jobs/new?project_id=${encodeURIComponent(project.id)}`}
              >
                Create Job
              </Link>
              <Link to={`/builds?project_id=${encodeURIComponent(project.id)}`}>
                View Project Builds
              </Link>
              <Link
                to={`/artifacts?project_id=${encodeURIComponent(project.id)}`}
              >
                Browse Project Artifacts
              </Link>
            </div>
          </section>

          <section className="detail-panel" aria-label="Artifacts and releases">
            <h3>Artifacts and Releases</h3>
            <p className="subtle-text">
              Use artifact views for published build outputs. The logical view
              is the lightweight entry point for versioned releases and
              channels.
            </p>
            <div className="detail-actions-row">
              <Link
                to={`/artifacts?project_id=${encodeURIComponent(project.id)}`}
              >
                Browse Project Artifacts
              </Link>
              <Link
                to={`/artifacts/logical?project_id=${encodeURIComponent(project.id)}`}
              >
                Open Release View
              </Link>
            </div>
          </section>

          <section className="settings-panel" style={{ marginTop: 16 }}>
            <h3>Project Members</h3>
            {memberError && <p className="error-text">{memberError}</p>}
            <form className="queue-build-form" onSubmit={onAddMember}>
              <label htmlFor="project-member-user">User ID</label>
              <input
                id="project-member-user"
                list="project-member-users"
                value={memberUserID}
                onChange={(event) => setMemberUserID(event.target.value)}
                disabled={addMemberMutation.isPending}
                placeholder="Existing user id"
              />
              <datalist id="project-member-users">
                {users?.map((user) => (
                  <option key={user.id} value={user.id}>
                    {user.email}
                  </option>
                ))}
              </datalist>
              <label htmlFor="project-member-role">Role</label>
              <select
                id="project-member-role"
                value={selectedRole}
                onChange={(event) =>
                  setSelectedRole(event.target.value as ProjectMemberRole)
                }
                disabled={addMemberMutation.isPending}
              >
                <option value="viewer">viewer</option>
                <option value="maintainer">maintainer</option>
                <option value="owner">owner</option>
              </select>
              <button type="submit" disabled={addMemberMutation.isPending}>
                {addMemberMutation.isPending ? "Adding…" : "Add Member"}
              </button>
            </form>

            {membersLoading && <p>Loading members…</p>}
            {membersError && (
              <p className="error-text">
                {formatAPIErrorMessage(
                  membersError,
                  "You do not have permission to view project members.",
                  "Failed to load members",
                )}
              </p>
            )}
            {members && members.length === 0 && (
              <p className="subtle-text">No project members yet.</p>
            )}
            {members && members.length > 0 && (
              <table className="table">
                <thead>
                  <tr>
                    <th>User</th>
                    <th>Display Name</th>
                    <th>Role</th>
                    <th>Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {members.map((member) => (
                    <tr key={member.user_id}>
                      <td>{member.email || member.user_id}</td>
                      <td>{member.display_name || "—"}</td>
                      <td>
                        <select
                          aria-label={`Role for ${member.email || member.user_id}`}
                          value={member.role}
                          onChange={(event) =>
                            updateMemberMutation.mutate({
                              userID: member.user_id,
                              role: event.target.value as ProjectMemberRole,
                            })
                          }
                          disabled={updateMemberMutation.isPending}
                        >
                          <option value="viewer">viewer</option>
                          <option value="maintainer">maintainer</option>
                          <option value="owner">owner</option>
                        </select>
                      </td>
                      <td>
                        <button
                          type="button"
                          className="table-action-button"
                          onClick={() =>
                            deleteMemberMutation.mutate(member.user_id)
                          }
                          disabled={deleteMemberMutation.isPending}
                        >
                          Remove
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </section>

          <section className="detail-panel" aria-label="Jobs in this project">
            <h3>Jobs in This Project</h3>
            {jobsLoading && <p>Loading jobs…</p>}
            {jobsError && (
              <p className="error-text">
                Failed to load jobs: {String(jobsError)}
              </p>
            )}
            {!jobsLoading && !jobsError && jobs && jobs.length === 0 && (
              <p className="subtle-text">No jobs in this project yet.</p>
            )}
            {jobs && jobs.length > 0 && (
              <table className="table">
                <thead>
                  <tr>
                    <th>Job</th>
                    <th>Repository</th>
                    <th>Default Ref</th>
                    <th>Updated</th>
                    <th>Created</th>
                    <th>Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {jobs.map((job) => (
                    <tr key={job.id}>
                      <td>
                        <Link to={`/jobs/${job.id}`}>{job.name}</Link>
                      </td>
                      <td>{job.repository_url}</td>
                      <td>{job.default_ref}</td>
                      <td>{formatTime(job.updated_at)}</td>
                      <td>{formatTime(job.created_at)}</td>
                      <td>
                        <div className="table-actions">
                          <Link to={`/jobs/${job.id}`}>Open Job</Link>
                          <Link
                            to={`/builds?project_id=${encodeURIComponent(project.id)}`}
                          >
                            Builds
                          </Link>
                          <Link
                            to={`/artifacts?project_id=${encodeURIComponent(project.id)}&job_id=${encodeURIComponent(job.id)}`}
                          >
                            Artifacts
                          </Link>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </section>
        </div>
        <BuildActivityRail scope={{ type: "project", projectId: project.id }} />
      </div>
    </>
  );
}
