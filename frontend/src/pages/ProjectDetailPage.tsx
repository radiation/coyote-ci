import { useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "react-router-dom";
import {
  deleteProjectMember,
  getProject,
  listJobsByProject,
  listProjectMembers,
  listUsers,
  updateProjectMember,
  upsertProjectMember,
} from "../api";
import type { ProjectMemberRole } from "../types/identity";
import { formatTime } from "../utils/time";

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
    onError: (mutationError) => setMemberError(String(mutationError)),
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
    onError: (mutationError) => setMemberError(String(mutationError)),
  });

  const deleteMemberMutation = useMutation({
    mutationFn: (userID: string) => deleteProjectMember(id!, userID),
    onMutate: () => setMemberError(null),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["projectMembers", id] });
    },
    onError: (mutationError) => setMemberError(String(mutationError)),
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
      <div className="page-header-row">
        <div>
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
          <Link to={`/artifacts?project_id=${encodeURIComponent(project.id)}`}>
            Browse Artifacts
          </Link>
        </div>
      </div>

      <div className="detail-grid">
        <div>
          <strong>ID</strong>
          <span>{project.id}</span>
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

      <section className="settings-panel" style={{ marginTop: 16 }}>
        <h3>Members</h3>
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
            Failed to load members: {String(membersError)}
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

      <h3>Jobs</h3>
      {jobsLoading && <p>Loading jobs…</p>}
      {jobsError && (
        <p className="error-text">Failed to load jobs: {String(jobsError)}</p>
      )}
      {!jobsLoading && !jobsError && jobs && jobs.length === 0 && (
        <p className="subtle-text">No jobs in this project yet.</p>
      )}
      {jobs && jobs.length > 0 && (
        <table className="table">
          <thead>
            <tr>
              <th>Name</th>
              <th>Repository</th>
              <th>Default Ref</th>
              <th>Updated</th>
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
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </>
  );
}
