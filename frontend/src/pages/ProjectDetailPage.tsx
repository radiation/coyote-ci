import { useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "react-router-dom";
import {
  deleteProjectMember,
  formatAPIErrorMessage,
  getProject,
  listJobsByProject,
  listProjectMembers,
  listUsers,
  updateProjectMember,
  upsertProjectMember,
} from "../api";
import { BuildActivityRail } from "../components/ScopedBuildActivityPanels";
import type { Job } from "../types/job";
import type { ProjectMemberRole } from "../types/identity";
import { formatTime } from "../utils/time";

type JobWithOptionalMetadata = Job & {
  slug?: string | null;
  description?: string | null;
};

export function ProjectDetailPage() {
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
            <p className="subtle-text">
              Job descriptions are shown when provided by the job API.
            </p>
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
                  {jobs.map((job) => {
                    // TODO: Remove fallback once job description/slug are guaranteed in project job responses.
                    const metadataJob = job as JobWithOptionalMetadata;
                    const slug = metadataJob.slug?.trim();
                    const description = metadataJob.description?.trim();

                    return (
                      <tr key={job.id}>
                        <td>
                          <div>
                            <Link to={`/jobs/${job.id}`}>{job.name}</Link>
                            <div className="subtle-text">
                              {slug ? `Slug: ${slug}` : `ID: ${job.id}`}
                            </div>
                            <div className="subtle-text">
                              {description || "Description unavailable."}
                            </div>
                          </div>
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
                    );
                  })}
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
