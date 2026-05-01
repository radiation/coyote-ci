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
  hasActiveFilters?: boolean;
  pageIndex?: number;
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

function artifactHeading(artifact: ArtifactBrowseItem): string {
  return artifact.name?.trim() || artifact.path;
}

function versionCountLabel(count: number): string {
  return `${count} version${count === 1 ? "" : "s"}`;
}

function firstVersionTag(
  version: ArtifactBrowseVersion | undefined,
): string | null {
  const tag = version?.version_tags?.[0]?.version?.trim();
  return tag || null;
}

export function ArtifactBrowser({
  artifacts,
  isLoading,
  error,
  hasActiveFilters = false,
  pageIndex = 0,
  onAssignVersion,
}: ArtifactBrowserProps) {
  const [expandedKeys, setExpandedKeys] = useState<string[]>([]);

  if (isLoading) {
    return (
      <div className="artifact-browser-list" aria-label="Loading artifacts">
        {Array.from({ length: 3 }, (_value, index) => (
          <div key={index} className="artifact-card artifact-card-skeleton">
            <div className="artifact-skeleton-line artifact-skeleton-title" />
            <div className="artifact-skeleton-line artifact-skeleton-meta" />
            <div className="artifact-skeleton-line artifact-skeleton-short" />
          </div>
        ))}
      </div>
    );
  }
  if (error) {
    return (
      <div className="empty-state artifact-empty-state artifact-error-state">
        <p className="error-text">Failed to load artifacts: {String(error)}</p>
      </div>
    );
  }
  if (!artifacts || artifacts.length === 0) {
    return (
      <div className="empty-state artifact-empty-state">
        <p className="empty">
          {pageIndex > 0
            ? "No artifacts on this page."
            : hasActiveFilters
              ? "No artifacts matched the current filters."
              : "No artifacts have been published yet."}
        </p>
        <p className="subtle-text">
          {hasActiveFilters
            ? "Adjust the search or type filter, or clear filters to return to the repository view."
            : "Run a build that publishes artifacts to populate this repository."}
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
        const latestTag = firstVersionTag(latestVersion);

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
                  <div className="artifact-card-kicker">
                    <span
                      className="artifact-expand-indicator"
                      aria-hidden="true"
                    >
                      {isExpanded ? "-" : "+"}
                    </span>
                    <span className="artifact-type-pill">
                      {TYPE_LABELS[artifact.artifact_type]}
                    </span>
                  </div>
                  <h3 className="artifact-card-title">
                    {artifactHeading(artifact)}
                  </h3>
                  {artifact.name && artifact.name !== artifact.path && (
                    <p className="subtle-text artifact-version-subtle">
                      {artifact.path}
                    </p>
                  )}
                  <div className="artifact-card-meta">
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
                  <span className="artifact-summary-primary">
                    {latestVersion
                      ? versionLabel(latestVersion)
                      : "No versions"}
                  </span>
                  {latestVersion && (
                    <StatusBadge status={latestVersion.build_status} />
                  )}
                  <span>{versionCountLabel(artifact.versions.length)}</span>
                  <span>{formatTime(artifact.latest_created_at)}</span>
                  {latestVersion && (
                    <span>{formatFileSize(latestVersion.size_bytes)}</span>
                  )}
                  {latestTag && (
                    <span className="version-tag-pill artifact-latest-tag">
                      {latestTag}
                    </span>
                  )}
                </div>
              </div>
            </button>

            {isExpanded && (
              <div className="artifact-card-body">
                <div className="artifact-detail-grid">
                  <div>
                    <strong>Name</strong>
                    <span>{artifactHeading(artifact)}</span>
                  </div>
                  <div>
                    <strong>Type</strong>
                    <span>{TYPE_LABELS[artifact.artifact_type]}</span>
                  </div>
                  <div>
                    <strong>Versions</strong>
                    <span>{versionCountLabel(artifact.versions.length)}</span>
                  </div>
                  <div>
                    <strong>Project</strong>
                    <span>{artifact.project_id}</span>
                  </div>
                  <div>
                    <strong>Latest Update</strong>
                    <span>{formatTime(artifact.latest_created_at)}</span>
                  </div>
                  <div className="artifact-version-meta-full">
                    <strong>Path</strong>
                    <span className="artifact-mono">{artifact.path}</span>
                  </div>
                </div>

                <div className="artifact-version-list-header">
                  <h4>Versions</h4>
                  <span className="subtle-text">Most recent first</span>
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
