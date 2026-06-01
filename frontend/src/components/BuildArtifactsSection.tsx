import type { ReactNode } from "react";
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
import { formatTime } from "../utils/time";
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

function buildLabel(build: Build | undefined, artifact: BuildArtifact): string {
  if (build?.build_number) {
    return `Build #${build.build_number}`;
  }
  return `Build ${artifact.build_id.slice(0, 8)}…`;
}

function projectLabel(build: Build | undefined): string {
  if (!build) {
    return "—";
  }
  const name = build.project_name?.trim() ?? "";
  const slug = build.project_slug?.trim() ?? "";
  return name || slug || build.project_id;
}

function jobLabel(build: Build | undefined): string {
  if (!build?.job_id) {
    return "—";
  }
  const name = build.job_name?.trim() ?? "";
  return name || `Job ${build.job_id.slice(0, 8)}…`;
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

function MetadataItem({
  label,
  value,
  mono = false,
}: {
  label: string;
  value: ReactNode;
  mono?: boolean;
}) {
  return (
    <div>
      <strong>{label}</strong>
      <span className={mono ? "artifact-mono" : undefined}>{value}</span>
    </div>
  );
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

        return (
          <article key={item.id} className="artifact-build-card">
            <div className="artifact-build-card-header">
              <div className="artifact-catalog-primary">
                <div className="artifact-card-kicker">
                  {typeLabel ? (
                    <span className="artifact-type-pill">{typeLabel}</span>
                  ) : null}
                  <span className="artifact-secondary-pill">
                    {stepScopeLabel(item, steps)}
                  </span>
                  <span className="artifact-secondary-pill">
                    {item.storage_provider}
                  </span>
                </div>
                <Link
                  className="artifact-build-link"
                  to={`/artifacts/${item.id}`}
                >
                  {artifactTitle(item)}
                </Link>
                {artifactSecondaryPath(item) ? (
                  <div className="subtle-text artifact-mono">
                    {artifactSecondaryPath(item)}
                  </div>
                ) : null}
              </div>
              <div className="artifact-actions artifact-build-actions">
                <Link to={`/artifacts/${item.id}`}>Open artifact</Link>
                <Link to={buildArtifactBrowserPath(item, build)}>
                  Repository view
                </Link>
                <a href={artifactDownloadURL(item.download_url_path)}>
                  Download
                </a>
              </div>
            </div>

            <div className="artifact-detail-grid artifact-build-card-grid">
              <MetadataItem label="Artifact path" value={item.path} mono />
              <MetadataItem
                label="Size"
                value={formatFileSize(item.size_bytes)}
              />
              <MetadataItem
                label="Digest"
                mono
                value={
                  item.checksum_sha256 ? (
                    <span
                      className="artifact-checksum-value"
                      title={item.checksum_sha256}
                    >
                      {formatChecksumDisplay(item.checksum_sha256)}
                    </span>
                  ) : (
                    "—"
                  )
                }
              />
              <MetadataItem
                label="Created"
                value={formatTime(item.created_at)}
              />
              <MetadataItem
                label="Versions"
                value={
                  versions.length > 0 ? (
                    <span className="version-tag-list artifact-inline-tag-list">
                      {versions.map((version) => (
                        <span
                          key={`${item.id}-version-${version}`}
                          className="version-tag-pill"
                        >
                          {version}
                        </span>
                      ))}
                    </span>
                  ) : (
                    "—"
                  )
                }
              />
              <MetadataItem
                label="Channels"
                value={
                  channels.length > 0 ? (
                    <span className="version-tag-list artifact-inline-tag-list">
                      {channels.map((channel) => (
                        <span
                          key={`${item.id}-channel-${channel}`}
                          className="version-tag-pill artifact-channel-pill"
                        >
                          {channel}
                        </span>
                      ))}
                    </span>
                  ) : (
                    "—"
                  )
                }
              />
              <MetadataItem
                label="Project"
                value={
                  build ? (
                    <Link to={`/projects/${build.project_id}`}>
                      {projectLabel(build)}
                    </Link>
                  ) : (
                    "—"
                  )
                }
              />
              <MetadataItem
                label="Build"
                value={
                  <Link to={`/builds/${item.build_id}`}>
                    {buildLabel(build, item)}
                  </Link>
                }
              />
              <MetadataItem
                label="Job"
                value={
                  build?.job_id ? (
                    <Link to={`/jobs/${build.job_id}`}>{jobLabel(build)}</Link>
                  ) : (
                    "—"
                  )
                }
              />
              <MetadataItem
                label="Source ref"
                value={
                  sourceRef ? (
                    sourceRefHref ? (
                      <a href={sourceRefHref}>{sourceRef}</a>
                    ) : (
                      sourceRef
                    )
                  ) : (
                    "—"
                  )
                }
              />
              <MetadataItem
                label="Commit"
                mono
                value={
                  sourceCommit ? (
                    sourceCommitHref ? (
                      <a href={sourceCommitHref}>{shortSHA(sourceCommit)}</a>
                    ) : (
                      shortSHA(sourceCommit)
                    )
                  ) : (
                    "—"
                  )
                }
              />
            </div>

            <div className="artifact-build-card-footer">
              <VersionTagEditor
                tags={item.version_tags ?? []}
                emptyText="No version or channel labels yet."
                inputLabel={`artifact-version-${item.id}`}
                onAssign={
                  onAssignVersion
                    ? (version) => onAssignVersion(item.id, version)
                    : undefined
                }
              />
            </div>
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
  if (isLoading) return <p>Loading artifacts…</p>;
  if (error)
    return (
      <p className="error-text">Failed to load artifacts: {String(error)}</p>
    );
  if (!artifacts || artifacts.length === 0) {
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
