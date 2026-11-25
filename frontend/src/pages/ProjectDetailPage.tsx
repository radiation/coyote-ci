import { useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Link,
  Navigate,
  useLocation,
  useNavigate,
  useParams,
} from "react-router-dom";
import {
  deleteProject,
  deleteProjectMember,
  formatAPIErrorMessage,
  getProject,
  getPublicProject,
  isAPIErrorStatus,
  listPublicBuilds,
  listProjectMembers,
  listUsers,
  updateProjectMember,
  upsertProjectMember,
} from "../api";
import { useAuth } from "../auth-context";
import {
  QueueActivityPanel,
  RecentBuildsPanel,
} from "../components/ScopedBuildActivityPanels";
import {
  ProjectJobsSection,
  ProjectBuildsSection,
} from "../components/ProjectOperationsSections";
import type { Project } from "../types/project";
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
  const { search, hash } = useLocation();
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
  if (authenticatedProject)
    return (
      <ProjectWorkspace
        key={authenticatedProject.id}
        project={authenticatedProject}
      />
    );
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
  if (publicProject && isAPIErrorStatus(authenticatedProjectError, 404)) {
    return (
      <Navigate
        to={{ pathname: `/projects/${publicProject.id}`, search, hash }}
        replace
      />
    );
  }
  if (publicProjectError && !isAPIErrorStatus(publicProjectError, 404)) {
    return (
      <p className="error-text">
        Failed to resolve public project: {String(publicProjectError)}
      </p>
    );
  }

  return <p className="error-text">Project not found.</p>;
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
          <p className="subtle-text">Slug: {project.slug}</p>
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

function ProjectWorkspace({ project }: { project: Project }) {
  const { pathname, search, hash } = useLocation();
  const views = ["Overview", "Jobs", "Builds", "Settings"];
  const view =
    views.find(
      (name) => name.toLowerCase() === new URLSearchParams(search).get("view"),
    ) ?? "Overview";
  const scope = { type: "project", projectId: project.id } as const;
  return (
    <>
      <Link to="/projects">← Back to projects</Link>
      <header>
        <h2>{project.name}</h2>
        <p className="subtle-text">Slug: {project.slug}</p>
      </header>
      <nav
        className="detail-actions-row job-view-nav"
        aria-label="Project views"
      >
        {views.map((name) => {
          const params = new URLSearchParams(search);
          if (name === "Overview") params.delete("view");
          else params.set("view", name.toLowerCase());
          return (
            <Link
              key={name}
              aria-current={name === view ? "page" : undefined}
              to={{ pathname, search: params.toString(), hash }}
            >
              {name}
            </Link>
          );
        })}
      </nav>
      {view === "Overview" && (
        <>
          {project.description && <p>{project.description}</p>}
          <div className="project-activity-grid">
            <QueueActivityPanel
              scope={scope}
              limit={4}
              title="Running and queued"
            />
            <RecentBuildsPanel
              scope={scope}
              limit={4}
              title="Recent results"
              resultsOnly
            />
          </div>
          <ProjectJobsSection id={project.id} compact />
          <div className="detail-actions-row">
            <Link
              to={`/artifacts?project_id=${encodeURIComponent(project.id)}`}
            >
              Browse Artifacts
            </Link>
            <Link
              to={`/artifacts/logical?project_id=${encodeURIComponent(project.id)}`}
            >
              Logical artifact repository
            </Link>
          </div>
        </>
      )}
      {view === "Jobs" && <ProjectJobsSection id={project.id} />}
      {view === "Builds" && <ProjectBuildsSection id={project.id} />}
      {view === "Settings" && <ProjectSettingsSection id={project.id} />}
    </>
  );
}

function ProjectSettingsSection({ id }: { id: string }) {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [memberUserID, setMemberUserID] = useState("");
  const [selectedRole, setSelectedRole] = useState<ProjectMemberRole>("viewer");
  const [memberError, setMemberError] = useState<string | null>(null);

  const [confirmDelete, setConfirmDelete] = useState(false);
  const deletion = useMutation({
    mutationFn: () => deleteProject(id),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["projects"] });
      navigate("/projects", { replace: true });
    },
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

  const { data: users, error: usersError } = useQuery({
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

  return (
    <>
      <section className="settings-panel" style={{ marginTop: 16 }}>
        <h3>Project Members</h3>
        {usersError && (
          <p className="error-text" role="status">
            User suggestions unavailable. Enter a known user ID.
          </p>
        )}
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

      <section className="detail-panel" aria-label="Delete project">
        <h3>Delete project</h3>
        {confirmDelete ? (
          <>
            <p>Permanently delete this project?</p>
            <button
              disabled={deletion.isPending}
              onClick={() => deletion.mutate()}
            >
              {deletion.isPending ? "Deleting…" : "Confirm deletion"}
            </button>
            <button
              disabled={deletion.isPending}
              onClick={() => setConfirmDelete(false)}
            >
              Cancel
            </button>
          </>
        ) : (
          <button onClick={() => setConfirmDelete(true)}>Delete project</button>
        )}
        {deletion.error && (
          <p role="alert" className="error-text">
            {formatAPIErrorMessage(
              deletion.error,
              "You do not have permission to delete this project.",
              "Failed to delete project",
            )}
          </p>
        )}
      </section>
    </>
  );
}
