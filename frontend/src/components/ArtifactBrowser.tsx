import { useState } from "react";
import { Link } from "react-router-dom";
import { artifactDownloadURL } from "../api";
import type { ArtifactBrowseItem, ArtifactBrowseVersion } from "../types";
import { formatFileSize } from "../utils/format";
import { formatTime } from "../utils/time";
import { StatusBadge } from "./StatusBadge";
import { VersionTagEditor } from "./VersionTagEditor";

interface ArtifactBrowserProps {
  artifacts: ArtifactBrowseItem[];
  isLoading: boolean;
  error: unknown;
  onAssignVersion?: (
    version: ArtifactBrowseVersion,
    releaseVersion: string,
  ) => Promise<void>;
}

const TYPE_LABELS: Record<ArtifactBrowseItem["artifact_type"], string> = {
  docker_image: "Docker image",
  npm_package: "npm package",
  generic: "Generic artifact",
  unknown: "Unknown",
};

function versionLabel(version: ArtifactBrowseVersion): string {
  if (version.build_number > 0) {
    return `Build #${version.build_number}`;
  }
  return `Build ${version.build_id.slice(0, 8)}…`;
}

function versionContext(version: ArtifactBrowseVersion): string {
  if (
    typeof version.step_index === "number" &&
    version.step_name &&
    version.step_name.trim()
  ) {
    return `Step ${version.step_index}: ${version.step_name}`;
  }
  if (typeof version.step_index === "number") {
    return `Step ${version.step_index}`;
  }
  return "Build-level artifact";
}

export function ArtifactBrowser({
  artifacts,
  isLoading,
  error,
  onAssignVersion,
}: ArtifactBrowserProps) {
  const [expandedKeys, setExpandedKeys] = useState<string[]>([]);

  if (isLoading) return <p>Loading artifacts…</p>;
  if (error) {
    return (
      <p className="error-text">Failed to load artifacts: {String(error)}</p>
    );
  }
  if (!artifacts || artifacts.length === 0) {
    return (
      <div className="empty-state artifact-empty-state">
        <p className="empty">No artifacts matched the current filters.</p>
        <p className="subtle-text">
          Adjust the search or type filter, or run builds that publish
          artifacts.
        </p>
      </div>
    );
  }

  function toggleExpanded(key: string) {
    setExpandedKeys((current) =>
      current.includes(key)
        ? current.filter((value) => value !== key)
        : [...current, key],
    );
  }

  return (
    <div className="artifact-browser-list">
      {artifacts.map((artifact) => {
        const isExpanded = expandedKeys.includes(artifact.key);
        const latestVersion = artifact.versions[0];

        return (
          <section key={artifact.key} className="artifact-card">
            <button
              type="button"
              className="artifact-card-toggle"
              onClick={() => toggleExpanded(artifact.key)}
              aria-expanded={isExpanded}
            >
              <div className="artifact-card-heading">
                <div>
                  <h3 className="artifact-card-title">{artifact.path}</h3>
                  <div className="artifact-card-meta">
                    <span className="artifact-type-pill">
                      {TYPE_LABELS[artifact.artifact_type]}
                    </span>
                    <span className="artifact-secondary-pill">
                      Project {artifact.project_id}
                    </span>
                    {artifact.job_id && (
                      <span className="artifact-secondary-pill">
                        Job {artifact.job_id.slice(0, 8)}…
                      </span>
                    )}
                  </div>
                </div>
                <div className="artifact-card-summary">
                  <span>{artifact.versions.length} version(s)</span>
                  <span>Latest {formatTime(artifact.latest_created_at)}</span>
                  {latestVersion && (
                    <span>{formatFileSize(latestVersion.size_bytes)}</span>
                  )}
                </div>
              </div>
            </button>

            {isExpanded && (
              <div className="artifact-card-body">
                <div className="artifact-detail-grid">
                  <div>
                    <strong>Type</strong>
                    <span>{TYPE_LABELS[artifact.artifact_type]}</span>
                  </div>
                  <div>
                    <strong>Versions</strong>
                    <span>{artifact.versions.length}</span>
                  </div>
                  <div>
                    <strong>Project</strong>
                    <span>{artifact.project_id}</span>
                  </div>
                  <div>
                    <strong>Latest Update</strong>
                    <span>{formatTime(artifact.latest_created_at)}</span>
                  </div>
                </div>

                <div className="artifact-version-list">
                  {artifact.versions.map((version) => (
                    <article
                      key={version.artifact_id}
                      className="artifact-version-row"
                    >
                      <div className="artifact-version-header">
                        <div>
                          <div className="artifact-version-title-row">
                            <Link to={`/builds/${version.build_id}`}>
                              {versionLabel(version)}
                            </Link>
                            <StatusBadge status={version.build_status} />
                          </div>
                          <p className="subtle-text artifact-version-subtle">
                            {versionContext(version)}
                          </p>
                        </div>
                        <div className="artifact-version-actions">
                          <a
                            href={artifactDownloadURL(
                              version.download_url_path,
                            )}
                          >
                            Download
                          </a>
                        </div>
                      </div>

                      <div className="artifact-version-meta-grid">
                        <div>
                          <strong>Created</strong>
                          <span>{formatTime(version.created_at)}</span>
                        </div>
                        <div>
                          <strong>Size</strong>
                          <span>{formatFileSize(version.size_bytes)}</span>
                        </div>
                        <div>
                          <strong>Storage</strong>
                          <span>{version.storage_provider}</span>
                        </div>
                        <div>
                          <strong>Content Type</strong>
                          <span>{version.content_type ?? "—"}</span>
                        </div>
                        <div className="artifact-version-meta-full">
                          <strong>Checksum</strong>
                          <span className="artifact-mono">
                            {version.checksum_sha256 ?? "—"}
                          </span>
                        </div>
                      </div>

                      <VersionTagEditor
                        tags={version.version_tags ?? []}
                        emptyText="No version tags yet."
                        inputLabel={`artifact-browser-version-${version.artifact_id}`}
                        onAssign={
                          onAssignVersion && version.job_id
                            ? (releaseVersion) =>
                                onAssignVersion(version, releaseVersion)
                            : undefined
                        }
                      />
                    </article>
                  ))}
                </div>
              </div>
            )}
          </section>
        );
      })}
    </div>
  );
}
