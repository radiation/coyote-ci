import { Link } from "react-router-dom";
import type { Project } from "../types/project";
import { formatTime } from "../utils/time";

export function ProjectList({ projects }: { projects: Project[] }) {
  return (
    <div className="project-card-grid">
      {projects.map((project) => (
        <article key={project.id} className="project-card">
          <div className="project-card-header">
            <div>
              <h3>
                <Link to={`/projects/${project.id}`}>{project.name}</Link>
              </h3>
              <p className="subtle-text">{project.slug}</p>
            </div>
            <Link className="secondary-button" to={`/projects/${project.id}`}>
              Open
            </Link>
          </div>
          <p className="project-card-description">
            {project.description?.trim() || "No description."}
          </p>
          <div className="project-card-footer">
            <span className="subtle-text">
              Updated {formatTime(project.updated_at)}
            </span>
            <Link to={`/builds?project_id=${encodeURIComponent(project.id)}`}>
              View builds
            </Link>
          </div>
        </article>
      ))}
    </div>
  );
}
