import { useState } from "react";
import { artifactDownloadURL } from "../api";
import { Link } from "react-router-dom";
import type {
  ArtifactType,
  Build,
  BuildArtifact,
  BuildStep,
  VersionTag,
} from "../types";
import {
  artifactSecondaryPath,
  artifactTitle,
  formatChecksumDisplay,
  formatFileSize,
} from "../utils/format";
import { VersionTagEditor } from "./VersionTagEditor";

const TYPE_LABELS: Record<ArtifactType, string> = {
  docker_image: "Docker image",
  npm_package: "npm package",
  generic: "Generic artifact",
  unknown: "Unknown",
};

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

function textValue(value: string | null | undefined): string | null {
  const trimmed = value?.trim();
  return trimmed ? trimmed : null;
}

function shortSHA(value: string | null | undefined): string {
  const trimmed = value?.trim() ?? "";
  if (!trimmed) {
    return "—";
  }
  return trimmed.slice(0, 12);
}

function sourceRefValue(build: Build | undefined): string | null {
  if (!build) {
    return null;
  }
  return (
    textValue(build.source_ref) ??
    textValue(build.source?.ref) ??
    textValue(build.trigger_ref)
  );
}

function sourceCommitValue(build: Build | undefined): string | null {
  if (!build) {
    return null;
  }
  return (
    textValue(build.source_sha) ??
    textValue(build.source_commit_sha) ??
    textValue(build.source?.source_commit_sha) ??
    textValue(build.trigger_commit_sha)
  );
}

function safeRepositoryURL(build: Build | undefined): URL | null {
  const candidate =
    textValue(build?.repository_url) ??
    textValue(build?.source?.repository_url);
  if (!candidate) {
    return null;
  }

  try {
    const parsed = new URL(candidate);
    if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
      return null;
    }
    return parsed;
  } catch {
    return null;
  }
}

function githubRepositoryBaseURL(build: Build | undefined): string | null {
  const repositoryURL = safeRepositoryURL(build);
  if (!repositoryURL || repositoryURL.hostname.toLowerCase() !== "github.com") {
    return null;
  }

  const [owner, repoWithSuffix] = repositoryURL.pathname
    .split("/")
    .filter(Boolean);
  const repo = repoWithSuffix?.replace(/\.git$/i, "");
  if (!owner || !repo) {
    return null;
  }

  return `${repositoryURL.origin}/${encodeURIComponent(owner)}/${encodeURIComponent(repo)}`;
}

function normalizeGitHubRef(ref: string): string {
  if (ref.startsWith("refs/heads/")) {
    return ref.slice("refs/heads/".length);
  }
  if (ref.startsWith("refs/tags/")) {
    return ref.slice("refs/tags/".length);
  }
  return ref;
}

function encodePathSegments(value: string): string {
  return value
    .split("/")
    .filter(Boolean)
    .map((segment) => encodeURIComponent(segment))
    .join("/");
}

function buildCommitURL(
  build: Build | undefined,
  sha: string | null,
): string | null {
  const baseURL = githubRepositoryBaseURL(build);
  if (!baseURL || !sha) {
    return null;
  }
  return `${baseURL}/commit/${encodeURIComponent(sha)}`;
}

function buildRefURL(
  build: Build | undefined,
  ref: string | null,
): string | null {
  const baseURL = githubRepositoryBaseURL(build);
  if (!baseURL || !ref) {
    return null;
  }
  return `${baseURL}/tree/${encodePathSegments(normalizeGitHubRef(ref))}`;
}

function artifactTypeLabel(item: BuildArtifact): string | null {
  if (!item.artifact_type) {
    return null;
  }
  return TYPE_LABELS[item.artifact_type] ?? item.artifact_type;
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
  const params = new URLSearchParams();
  params.set("q", item.path);
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
        const sourceRef = sourceRefValue(build);
        const sourceRefHref = buildRefURL(build, sourceRef);
        const sourceCommit = sourceCommitValue(build);
        const sourceCommitHref = buildCommitURL(build, sourceCommit);
        const typeLabel = artifactTypeLabel(item);
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
                    <span
                      key={`${item.id}-version-${version}`}
                      className="version-tag-pill"
                    >
                      {version}
                    </span>
                  ))}
                  {channels.map((channel) => (
                    <span
                      key={`${item.id}-channel-${channel}`}
                      className="version-tag-pill artifact-channel-pill"
                    >
                      {channel}
                    </span>
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
