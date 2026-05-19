import { useQuery } from "@tanstack/react-query";
import { Link, useParams } from "react-router-dom";
import { artifactDownloadURL, getArtifact } from "../api";
import { StatusBadge } from "../components/StatusBadge";
import type { ArtifactDetail } from "../types";
import { formatFileSize } from "../utils/format";
import { formatTime } from "../utils/time";

function artifactTitle(artifact: ArtifactDetail): string {
  return artifact.name?.trim() || artifact.path;
}

function artifactHeaderPath(artifact: ArtifactDetail): string | null {
  const trimmedName = artifact.name?.trim() ?? "";
  if (!trimmedName || trimmedName === artifact.path) {
    return null;
  }
  return artifact.path;
}

function buildLabel(artifact: ArtifactDetail): string {
  if (artifact.build_number > 0) {
    return `Build #${artifact.build_number}`;
  }
  return `Build ${artifact.build_id.slice(0, 8)}…`;
}

function jobLabel(artifact: ArtifactDetail): string {
  const name = artifact.job_name?.trim() ?? "";
  if (name) {
    return name;
  }
  const id = artifact.job_id?.trim() ?? "";
  if (!id) {
    return "—";
  }
  return `${id.slice(0, 8)}…`;
}

function stepLabel(artifact: ArtifactDetail): string {
  if (
    typeof artifact.step_index === "number" &&
    artifact.step_name &&
    artifact.step_name.trim()
  ) {
    return `Step ${artifact.step_index}: ${artifact.step_name}`;
  }
  if (typeof artifact.step_index === "number") {
    return `Step ${artifact.step_index}`;
  }
  return "Build-level artifact";
}

function projectLabel(artifact: ArtifactDetail): string {
  const name = artifact.project_name?.trim() ?? "";
  const slug = artifact.project_slug?.trim() ?? "";
  if (name && slug) {
    return `${name} (${slug})`;
  }
  return name || slug || artifact.project_id;
}

export function ArtifactDetailPage() {
  const { id } = useParams<{ id: string }>();
  const { data, isLoading, error } = useQuery({
    queryKey: ["artifact", id],
    queryFn: () => getArtifact(id!),
    enabled: !!id,
  });

  if (isLoading) {
    return <p>Loading artifact…</p>;
  }
  if (error) {
    return (
      <p className="error-text">Failed to load artifact: {String(error)}</p>
    );
  }
  if (!data) {
    return <p className="error-text">Artifact not found.</p>;
  }

  return (
    <>
      <Link to="/artifacts">← Back to artifacts</Link>

      <section className="detail-panel artifact-detail-header">
        <div>
          <h2>{artifactTitle(data)}</h2>
          {artifactHeaderPath(data) && (
            <p className="subtle-text artifact-mono">
              {artifactHeaderPath(data)}
            </p>
          )}
        </div>
        <div className="artifact-actions">
          <a
            className="secondary-button"
            href={artifactDownloadURL(data.download_url_path)}
          >
            Download
          </a>
        </div>
      </section>

      <div className="detail-summary">
        <span>
          <strong>Build:</strong>{" "}
          <Link to={`/builds/${data.build_id}`}>{buildLabel(data)}</Link>
        </span>
        <span>
          <strong>Status:</strong> <StatusBadge status={data.build_status} />
        </span>
        <span>
          <strong>Job:</strong>{" "}
          {data.job_id ? (
            <Link to={`/jobs/${data.job_id}`}>{jobLabel(data)}</Link>
          ) : (
            "—"
          )}
        </span>
        <span>
          <strong>Step:</strong> {stepLabel(data)}
        </span>
      </div>

      <section className="detail-panel">
        <h3>Artifact Metadata</h3>
        <div className="artifact-detail-grid">
          <div>
            <strong>Name</strong>
            <span>{artifactTitle(data)}</span>
          </div>
          <div>
            <strong>Type</strong>
            <span>{data.artifact_type}</span>
          </div>
          <div>
            <strong>Size</strong>
            <span>{formatFileSize(data.size_bytes)}</span>
          </div>
          <div>
            <strong>Created</strong>
            <span>{formatTime(data.created_at)}</span>
          </div>
          <div>
            <strong>Project</strong>
            <span>{projectLabel(data)}</span>
          </div>
          <div>
            <strong>Build</strong>
            <span>
              <Link to={`/builds/${data.build_id}`}>{buildLabel(data)}</Link>
            </span>
          </div>
          <div>
            <strong>Job</strong>
            <span>
              {data.job_id ? (
                <Link to={`/jobs/${data.job_id}`}>{jobLabel(data)}</Link>
              ) : (
                "—"
              )}
            </span>
          </div>
          <div>
            <strong>Step</strong>
            <span>{stepLabel(data)}</span>
          </div>
          <div>
            <strong>Storage</strong>
            <span>{data.storage_provider}</span>
          </div>
          <div>
            <strong>Content Type</strong>
            <span>{data.content_type ?? "—"}</span>
          </div>
          <div className="artifact-version-meta-full">
            <strong>Checksum</strong>
            <span className="artifact-mono">{data.checksum_sha256 ?? "—"}</span>
          </div>
        </div>
      </section>
    </>
  );
}
