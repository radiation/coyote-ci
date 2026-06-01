import { useState } from "react";
import { Link } from "react-router-dom";
import { artifactDownloadURL } from "../api";
import type {
  ArtifactBrowseItem,
  ArtifactBrowseVersion,
  VersionTag,
} from "../types";
import { artifactTypeLabel, formatFileSize } from "../utils/format";
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

function tagKind(tag: VersionTag): "version" | "channel" {
  return tag.kind === "channel" ? "channel" : "version";
}

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

function artifactProjectLabel(artifact: ArtifactBrowseItem): string {
  const name = artifact.project_name?.trim() ?? "";
  const slug = artifact.project_slug?.trim() ?? "";
  if (name && slug) {
    return `${name} (${slug})`;
  }
  return name || slug || artifact.project_id;
}

function artifactJobLabel(artifact: ArtifactBrowseItem): string {
  const name = artifact.job_name?.trim() ?? "";
  if (name) {
    return name;
  }
  const id = artifact.job_id?.trim() ?? "";
  if (!id) {
    return "";
  }
  return `${id.slice(0, 8)}…`;
}

function versionProjectLabel(version: ArtifactBrowseVersion): string {
  const name = version.project_name?.trim() ?? "";
  const slug = version.project_slug?.trim() ?? "";
  if (name && slug) {
    return `${name} (${slug})`;
  }
  return name || slug || version.project_id;
}

function versionJobLabel(version: ArtifactBrowseVersion): string {
  const name = version.job_name?.trim() ?? "";
  if (name) {
    return name;
  }
  const id = version.job_id?.trim() ?? "";
  if (!id) {
    return "—";
  }
  return `${id.slice(0, 8)}…`;
}

function firstVersionTag(
  version: ArtifactBrowseVersion | undefined,
): string | null {
  const tag = version?.version_tags
    ?.find((item) => tagKind(item) === "version")
    ?.version?.trim();
  return tag || null;
}

function versionTags(version: ArtifactBrowseVersion): VersionTag[] {
  return (version.version_tags ?? []).filter(
    (tag) => tagKind(tag) === "version",
  );
}

function channelTags(version: ArtifactBrowseVersion): VersionTag[] {
  return (version.version_tags ?? []).filter(
    (tag) => tagKind(tag) === "channel",
  );
}

function channelResolutionLabel(version: ArtifactBrowseVersion): string {
  const labels = versionTags(version)
    .map((tag) => tag.version.trim())
    .filter(Boolean);
  if (labels.length > 0) {
    return labels.join(", ");
  }
  return versionLabel(version);
}

function channelCountLabel(count: number): string {
  return `${count} channel${count === 1 ? "" : "s"}`;
}

function totalChannelCount(artifact: ArtifactBrowseItem): number {
  return artifact.versions.reduce(
    (count, version) => count + channelTags(version).length,
    0,
  );
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
              ? "No release artifacts matched the current filters."
              : "No versioned artifacts or channels yet."}
        </p>
        <p className="subtle-text">
          {hasActiveFilters
            ? "Adjust the search or filters, or clear them to return to the full release view."
            : "Publish artifact versions or channels from a build to populate this release view."}
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
        const channels = isExpanded
          ? artifact.versions.flatMap((version) =>
              channelTags(version).map((tag) => ({ tag, version })),
            )
          : [];
        const channelCount = isExpanded
          ? channels.length
          : totalChannelCount(artifact);

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
                      {artifactTypeLabel(artifact.artifact_type)}
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
                      Project {artifactProjectLabel(artifact)}
                    </span>
                    {artifact.job_id && (
                      <span className="artifact-secondary-pill">
                        Job {artifactJobLabel(artifact)}
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
                  <span>{channelCountLabel(channelCount)}</span>
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
                    <span>{artifactTypeLabel(artifact.artifact_type)}</span>
                  </div>
                  <div>
                    <strong>Versions</strong>
                    <span>{versionCountLabel(artifact.versions.length)}</span>
                  </div>
                  <div>
                    <strong>Channels</strong>
                    <span>{channelCountLabel(channelCount)}</span>
                  </div>
                  <div>
                    <strong>Project</strong>
                    <span>{artifactProjectLabel(artifact)}</span>
                  </div>
                  <div>
                    <strong>Job</strong>
                    <span>
                      {artifact.job_id ? artifactJobLabel(artifact) : "—"}
                    </span>
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

                <section className="artifact-release-section">
                  <div className="artifact-version-list-header artifact-release-section-header">
                    <h4>Channels</h4>
                    <span className="subtle-text">Mutable aliases</span>
                  </div>

                  {channels.length > 0 ? (
                    <div className="artifact-channel-list">
                      {channels.map(({ tag, version }) => (
                        <article key={tag.id} className="artifact-channel-row">
                          <div className="artifact-channel-copy">
                            <div className="artifact-channel-header">
                              <span className="version-tag-pill artifact-channel-pill">
                                {tag.version}
                              </span>
                              <span className="subtle-text">
                                Points to {channelResolutionLabel(version)}
                              </span>
                            </div>
                            <div className="artifact-channel-meta subtle-text">
                              <span>
                                Artifact{" "}
                                <Link to={`/artifacts/${version.artifact_id}`}>
                                  {artifactHeading(artifact)}
                                </Link>
                              </span>
                              <span>
                                Build{" "}
                                <Link to={`/builds/${version.build_id}`}>
                                  {versionLabel(version)}
                                </Link>
                              </span>
                              <span>{versionProjectLabel(version)}</span>
                              <span>{versionJobLabel(version)}</span>
                              <span>{formatTime(version.created_at)}</span>
                            </div>
                          </div>
                          <div className="artifact-actions artifact-channel-actions">
                            <Link to={`/artifacts/${version.artifact_id}`}>
                              Open
                            </Link>
                            <a
                              href={artifactDownloadURL(
                                version.download_url_path,
                              )}
                            >
                              Download
                            </a>
                          </div>
                        </article>
                      ))}
                    </div>
                  ) : (
                    <p className="subtle-text artifact-release-empty">
                      No channels currently point to this artifact package.
                    </p>
                  )}
                </section>

                <div className="artifact-version-list-header artifact-release-section-header">
                  <h4>Versions</h4>
                  <span className="subtle-text">
                    Immutable releases, most recent first
                  </span>
                </div>

                <div className="artifact-version-list">
                  {artifact.versions.map((version) => {
                    const releaseTags = versionTags(version);
                    const channelsPointingHere = channelTags(version);

                    return (
                      <article
                        key={version.artifact_id}
                        className="artifact-version-row"
                      >
                        <div className="artifact-version-header">
                          <div>
                            <div className="artifact-version-title-row">
                              <Link to={`/artifacts/${version.artifact_id}`}>
                                {artifactHeading(artifact)}
                              </Link>
                              <StatusBadge status={version.build_status} />
                            </div>
                            <p className="subtle-text artifact-version-subtle">
                              {versionContext(version)}
                            </p>
                          </div>
                          <div className="artifact-actions artifact-version-actions">
                            <Link to={`/artifacts/${version.artifact_id}`}>
                              Open artifact
                            </Link>
                            <a
                              href={artifactDownloadURL(
                                version.download_url_path,
                              )}
                            >
                              Download
                            </a>
                          </div>
                        </div>

                        <div className="artifact-version-badge-groups">
                          <div className="artifact-version-badge-group">
                            <strong>Versions</strong>
                            <div
                              className="version-tag-list"
                              aria-label={`artifact-version-labels-${version.artifact_id}`}
                            >
                              {releaseTags.length > 0 ? (
                                releaseTags.map((tag) => (
                                  <span
                                    key={tag.id}
                                    className="version-tag-pill"
                                  >
                                    {tag.version}
                                  </span>
                                ))
                              ) : (
                                <span className="subtle-text">
                                  No versions yet
                                </span>
                              )}
                            </div>
                          </div>
                          <div className="artifact-version-badge-group">
                            <strong>Channels</strong>
                            <div
                              className="version-tag-list"
                              aria-label={`artifact-version-channels-${version.artifact_id}`}
                            >
                              {channelsPointingHere.length > 0 ? (
                                channelsPointingHere.map((tag) => (
                                  <span
                                    key={tag.id}
                                    className="version-tag-pill artifact-channel-pill"
                                  >
                                    {tag.version}
                                  </span>
                                ))
                              ) : (
                                <span className="subtle-text">
                                  No channels yet
                                </span>
                              )}
                            </div>
                          </div>
                        </div>

                        <div className="artifact-version-meta-grid">
                          <div>
                            <strong>Created</strong>
                            <span>{formatTime(version.created_at)}</span>
                          </div>
                          <div>
                            <strong>Build</strong>
                            <span>
                              <Link to={`/builds/${version.build_id}`}>
                                {versionLabel(version)}
                              </Link>
                            </span>
                          </div>
                          <div>
                            <strong>Project</strong>
                            <span>{versionProjectLabel(version)}</span>
                          </div>
                          <div>
                            <strong>Job</strong>
                            <span>{versionJobLabel(version)}</span>
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
                            <strong>Digest</strong>
                            <span
                              className="artifact-mono artifact-checksum-value"
                              title={version.checksum_sha256 ?? undefined}
                            >
                              {version.checksum_sha256 ?? "—"}
                            </span>
                          </div>
                        </div>

                        <VersionTagEditor
                          tags={releaseTags}
                          emptyText="No versions yet."
                          inputLabel={`artifact-browser-version-${version.artifact_id}`}
                          submitLabel="Assign version"
                          onAssign={
                            onAssignVersion && version.job_id
                              ? (releaseVersion) =>
                                  onAssignVersion(version, releaseVersion)
                              : undefined
                          }
                        />
                      </article>
                    );
                  })}
                </div>
              </div>
            )}
          </section>
        );
      })}
    </div>
  );
}
