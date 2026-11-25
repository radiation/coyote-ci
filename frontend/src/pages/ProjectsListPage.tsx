import { useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "react-router-dom";
import { createProject, listProjects, listPublicProjects } from "../api";
import { useAuth } from "../auth-context";
import { formatTime } from "../utils/time";

export function ProjectsListPage() {
  const { authStatus } = useAuth();

  return authStatus === "unauthenticated" ? (
    <PublicProjectsListPage />
  ) : (
    <AuthenticatedProjectsListPage />
  );
}

function PublicProjectsListPage() {
  const {
    data: projects,
    isLoading,
    error,
  } = useQuery({
    queryKey: ["publicProjects"],
    queryFn: listPublicProjects,
  });

  return (
    <>
      <h2>Public Projects</h2>
      <p className="subtle-text">Browse projects that are shared publicly.</p>
      {isLoading && <p>Loading projects…</p>}
      {error && (
        <p className="error-text">Failed to load projects: {String(error)}</p>
      )}
      {!isLoading && !error && projects?.length === 0 && (
        <p className="empty">No public projects are available.</p>
      )}
      {projects && projects.length > 0 && (
        <table className="table">
          <thead>
            <tr>
              <th>Name</th>
              <th>Slug</th>
              <th>Description</th>
            </tr>
          </thead>
          <tbody>
            {projects.map((project) => (
              <tr key={project.id}>
                <td>
                  <Link to={`/projects/${project.slug}`}>{project.name}</Link>
                </td>
                <td>{project.slug}</td>
                <td>{project.description || "—"}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </>
  );
}

function AuthenticatedProjectsListPage() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [name, setName] = useState("");
  const [filter, setFilter] = useState("");
  const [slug, setSlug] = useState("");
  const [description, setDescription] = useState("");
  const [errorMessage, setErrorMessage] = useState<string | null>(null);

  const {
    data: projects,
    isLoading,
    error,
    dataUpdatedAt,
  } = useQuery({
    queryKey: ["projects"],
    queryFn: listProjects,
  });

  const createMutation = useMutation({
    mutationFn: createProject,
    onMutate: () => {
      setErrorMessage(null);
    },
    onSuccess: async (project) => {
      await queryClient.invalidateQueries({ queryKey: ["projects"] });
      navigate(`/projects/${project.id}`);
    },
    onError: (mutationError) => {
      setErrorMessage(`Failed to create project: ${String(mutationError)}`);
    },
  });

  const visible = projects?.filter((project) =>
    `${project.name} ${project.slug}`
      .toLowerCase()
      .includes(filter.trim().toLowerCase()),
  );

  const onSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();

    const trimmedName = name.trim();
    const trimmedSlug = slug.trim();
    const trimmedDescription = description.trim();

    if (!trimmedName || !trimmedSlug) {
      setErrorMessage("Name and slug are required.");
      return;
    }

    createMutation.mutate({
      name: trimmedName,
      slug: trimmedSlug,
      description: trimmedDescription || undefined,
    });
  };

  return (
    <>
      <div className="page-header-row">
        <div>
          <h2>Projects</h2>
          <p className="subtle-text">
            Last updated:{" "}
            {dataUpdatedAt > 0
              ? formatTime(new Date(dataUpdatedAt).toISOString())
              : "—"}
          </p>
        </div>
      </div>

      <label>
        Find a project{" "}
        <input
          type="search"
          value={filter}
          onChange={(event) => setFilter(event.target.value)}
        />
      </label>
      <details className="project-create">
        <summary>Create Project</summary>

        <form className="job-form" onSubmit={onSubmit}>
          <label htmlFor="project-name">Name</label>
          <input
            id="project-name"
            value={name}
            onChange={(event) => setName(event.target.value)}
            disabled={createMutation.isPending}
            placeholder="Platform"
          />

          <label htmlFor="project-slug">Slug</label>
          <input
            id="project-slug"
            value={slug}
            onChange={(event) => setSlug(event.target.value)}
            disabled={createMutation.isPending}
            placeholder="platform"
          />

          <label htmlFor="project-description">Description</label>
          <textarea
            id="project-description"
            value={description}
            onChange={(event) => setDescription(event.target.value)}
            disabled={createMutation.isPending}
            rows={3}
            placeholder="Optional context for the jobs grouped under this project."
          />

          <button type="submit" disabled={createMutation.isPending}>
            {createMutation.isPending ? "Creating…" : "Create Project"}
          </button>
        </form>
      </details>

      {errorMessage && <p className="error-text">{errorMessage}</p>}
      {isLoading && <p>Loading projects…</p>}
      {error && (
        <p className="error-text">Failed to load projects: {String(error)}</p>
      )}

      {!isLoading && !error && projects && projects.length === 0 && (
        <div className="empty-state">
          <p className="empty">No projects yet.</p>
          <p className="subtle-text">
            Create a project before assigning jobs to a durable grouping.
          </p>
        </div>
      )}

      {projects && projects.length > 0 && !visible?.length && (
        <p>No matching projects.</p>
      )}
      {visible && visible.length > 0 && (
        <table className="table">
          <thead>
            <tr>
              <th>Name</th>
              <th>Slug</th>
              <th>Description</th>
              <th>Updated</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            {visible.map((project) => (
              <tr key={project.id}>
                <td>
                  <Link to={`/projects/${project.id}`}>{project.name}</Link>
                </td>
                <td>{project.slug}</td>
                <td>{project.description || "—"}</td>
                <td>{formatTime(project.updated_at)}</td>
                <td>
                  <Link
                    aria-label={`Settings for ${project.name}`}
                    to={`/projects/${project.id}?view=settings`}
                  >
                    Settings
                  </Link>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </>
  );
}
