import { useState } from "react";
import { artifactDownloadURL } from "../api";
import { Link } from "react-router-dom";
import type { Build, BuildArtifact, BuildStep, VersionTag } from "../types";
import {
  artifactTypeLabel,
  artifactSecondaryPath,
  artifactTitle,
  formatChecksumDisplay,
  formatFileSize,
} from "../utils/format";
import {
  buildGitHubCommitURL,
  buildGitHubRefURL,
  buildPrimaryCommitValue,
  buildSourceRefValue,
  shortSHA,
} from "../utils/provenance";
import { VersionTagEditor } from "./VersionTagEditor";

interface Props {
  build?: Build;
  artifacts: BuildArtifact[];
  steps?: BuildStep[];
  isLoading: boolean;
  error: unknown;
  onAssignVersion?: (artifactID: string, version: string) => Promise<void>;
}

function tagKind(tag: VersionTag): "version" | "channel" {
  return tag.kind === "channel" ? "channel" : "version";
}

function buildArtifactTypeLabel(item: BuildArtifact): string | null {
  if (!item.artifact_type) {
    return null;
  }
  return artifactTypeLabel(item.artifact_type);
}

function stepScopeLabel(
  item: BuildArtifact,
  steps: BuildStep[] | undefined,
): string {
  return item.step_id ? stepLabel(item.step_id, steps) : "Build-level artifact";
}

function buildArtifactBrowserPath(
  item: BuildArtifact,
  build: Build | undefined,
): string {
  return buildArtifactScopedBrowserPath(item.path, build);
}

function buildArtifactScopedBrowserPath(
  queryValue: string,
  build: Build | undefined,
): string {
  const params = new URLSearchParams();
  params.set("q", queryValue);
  if (build?.job_id) {
    params.set("job_id", build.job_id);
  } else if (build?.project_id) {
    params.set("project_id", build.project_id);
  }
  const query = params.toString();
  return query ? `/artifacts/logical?${query}` : "/artifacts/logical";
}

function tagValues(item: BuildArtifact, kind: "version" | "channel"): string[] {
  return (item.version_tags ?? [])
    .filter((tag) => tagKind(tag) === kind)
    .map((tag) => tag.version)
    .filter(Boolean);
}

function stepLabel(stepId: string, steps: BuildStep[] | undefined): string {
  if (steps) {
    const step = steps.find((s) => s.id === stepId);
    if (step) return `Step ${step.step_index}: ${step.name}`;
  }
  return `Step ${stepId.slice(0, 8)}…`;
}

function sortArtifacts(items: BuildArtifact[]): BuildArtifact[] {
  return [...items].sort((left, right) => {
    const createdCompare = right.created_at.localeCompare(left.created_at);
    if (createdCompare !== 0) {
      return createdCompare;
    }
    return artifactTitle(left).localeCompare(artifactTitle(right));
  });
}

function ArtifactTable({
  build,
  steps,
  items,
  onAssignVersion,
}: {
  build?: Build;
  steps?: BuildStep[];
  items: BuildArtifact[];
  onAssignVersion?: (artifactID: string, version: string) => Promise<void>;
}) {
  const [openAssignID, setOpenAssignID] = useState<string | null>(null);

  return (
    <div className="artifact-build-list">
      {sortArtifacts(items).map((item) => {
        const versions = tagValues(item, "version");
        const channels = tagValues(item, "channel");
        const sourceRef = buildSourceRefValue(build);
        const sourceRefHref = buildGitHubRefURL(build, sourceRef);
        const sourceCommit = buildPrimaryCommitValue(build);
        const sourceCommitHref = buildGitHubCommitURL(build, sourceCommit);
        const typeLabel = buildArtifactTypeLabel(item);
        const compactPath = artifactSecondaryPath(item) ?? item.path;
        const hasLabels = versions.length > 0 || channels.length > 0;
        const showAssignEditor =
          openAssignID === item.id && Boolean(onAssignVersion);
        const provenanceLabel =
          sourceRef ?? (sourceCommit ? shortSHA(sourceCommit) : null);
        const provenanceHref = sourceRef ? sourceRefHref : sourceCommitHref;
        const needsScopeBadge = !item.step_id;

        return (
          <article
            key={item.id}
            className="artifact-build-card artifact-build-card-compact"
          >
            <div className="artifact-build-card-header">
              <div className="artifact-build-summary">
                <div className="artifact-card-kicker">
                  {typeLabel ? (
                    <span className="artifact-type-pill">{typeLabel}</span>
                  ) : null}
                  {needsScopeBadge ? (
                    <span className="artifact-secondary-pill">
                      {stepScopeLabel(item, steps)}
                    </span>
                  ) : null}
                  {versions.map((version) => (
                    <Link
                      key={`${item.id}-version-${version}`}
                      className="version-tag-pill"
                      to={buildArtifactScopedBrowserPath(version, build)}
                    >
                      {version}
                    </Link>
                  ))}
                  {channels.map((channel) => (
                    <Link
                      key={`${item.id}-channel-${channel}`}
                      className="version-tag-pill artifact-channel-pill"
                      to={buildArtifactScopedBrowserPath(channel, build)}
                    >
                      {channel}
                    </Link>
                  ))}
                </div>
                <div className="artifact-build-copy">
                  <Link
                    className="artifact-build-link"
                    to={`/artifacts/${item.id}`}
                  >
                    {artifactTitle(item)}
                  </Link>
                  <div className="artifact-build-meta subtle-text">
                    <span className="artifact-mono">{compactPath}</span>
                    <span aria-hidden="true">·</span>
                    <span>{formatFileSize(item.size_bytes)}</span>
                    {item.checksum_sha256 ? (
                      <>
                        <span aria-hidden="true">·</span>
                        <span
                          className="artifact-mono artifact-checksum-value"
                          title={item.checksum_sha256}
                        >
                          {formatChecksumDisplay(item.checksum_sha256)}
                        </span>
                      </>
                    ) : null}
                    {provenanceLabel ? (
                      <>
                        <span aria-hidden="true">·</span>
                        {provenanceHref ? (
                          <a href={provenanceHref}>{provenanceLabel}</a>
                        ) : (
                          <span>{provenanceLabel}</span>
                        )}
                      </>
                    ) : null}
                  </div>
                </div>
              </div>
              <div className="artifact-actions artifact-build-actions">
                <Link to={`/artifacts/${item.id}`}>Open artifact</Link>
                <Link to={buildArtifactBrowserPath(item, build)}>
                  Repository view
                </Link>
                <a href={artifactDownloadURL(item.download_url_path)}>
                  Download
                </a>
                {onAssignVersion ? (
                  <button
                    type="button"
                    className="inline-action-button artifact-assign-toggle"
                    onClick={() =>
                      setOpenAssignID((current) =>
                        current === item.id ? null : item.id,
                      )
                    }
                  >
                    {showAssignEditor ? "Hide version" : "Assign version"}
                  </button>
                ) : null}
              </div>
            </div>
            {showAssignEditor ? (
              <div className="artifact-build-card-footer artifact-build-card-editor">
                {!hasLabels ? (
                  <p className="subtle-text artifact-build-empty-copy">
                    No versions or channels yet.
                  </p>
                ) : null}
                <VersionTagEditor
                  tags={item.version_tags ?? []}
                  emptyText="No versions or channels yet."
                  inputLabel={`artifact-version-${item.id}`}
                  submitLabel="Save version"
                  onAssign={
                    onAssignVersion
                      ? (version) => onAssignVersion(item.id, version)
                      : undefined
                  }
                />
              </div>
            ) : null}
          </article>
        );
      })}
    </div>
  );
}

export function BuildArtifactsSection({
  build,
  artifacts,
  steps,
  isLoading,
  error,
  onAssignVersion,
}: Props) {
  if (isLoading) {
    return <p className="subtle-text">Loading artifacts…</p>;
  }

  if (error) {
    return (
      <p className="error-text">Failed to load artifacts: {String(error)}</p>
    );
  }

  if (artifacts.length === 0) {
    return (
      <p className="subtle-text">
        No artifacts were collected for this build. Check packaging or upload
        steps in the execution timeline, then rerun if you expected published
        outputs.
      </p>
    );
  }

  const shared = artifacts.filter((a) => !a.step_id);
  const stepIndexByID = new Map(
    (steps ?? []).map((step) => [step.id, step.step_index]),
  );
  const byStep = new Map<string, BuildArtifact[]>();
  for (const a of artifacts) {
    if (a.step_id) {
      const list = byStep.get(a.step_id) ?? [];
      list.push(a);
      byStep.set(a.step_id, list);
    }
  }

  const stepEntries = [...byStep.entries()].sort((a, b) => {
    const leftIndex = stepIndexByID.get(a[0]) ?? Number.MAX_SAFE_INTEGER;
    const rightIndex = stepIndexByID.get(b[0]) ?? Number.MAX_SAFE_INTEGER;
    if (leftIndex !== rightIndex) {
      return leftIndex - rightIndex;
    }
    return a[0].localeCompare(b[0]);
  });

  return (
    <div className="artifacts-section">
      {shared.length > 0 && (
        <div className="artifact-group">
          <h4 className="artifact-group-label">Build-level</h4>
          <ArtifactTable
            build={build}
            steps={steps}
            items={shared}
            onAssignVersion={onAssignVersion}
          />
        </div>
      )}
      {stepEntries.map(([stepId, items]) => (
        <div key={stepId} className="artifact-group">
          <h4 className="artifact-group-label">{stepLabel(stepId, steps)}</h4>
          <ArtifactTable
            build={build}
            steps={steps}
            items={items}
            onAssignVersion={onAssignVersion}
          />
        </div>
      ))}
    </div>
  );
}
