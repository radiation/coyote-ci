import { useQuery } from "@tanstack/react-query";
import { Link, useParams } from "react-router-dom";
import {
  artifactDownloadURL,
  getArtifact,
  getBuild,
  getBuildArtifacts,
} from "../api";
import { StatusBadge } from "../components/StatusBadge";
import { APIError } from "../api/request";
import type {
  ArtifactDetail,
  ArtifactType,
  BuildArtifact,
  VersionTag,
} from "../types";
import {
  artifactSecondaryPath,
  artifactTitle as formatArtifactTitle,
  formatChecksumDisplay,
  formatFileSize,
} from "../utils/format";
import { formatTime } from "../utils/time";
import {
  buildGitHubCommitURL,
  buildGitHubRefURL,
  buildLabel as buildPageLabel,
  buildPrimaryCommitValue,
  buildSourceRefValue,
  jobLabel as buildPageJobLabel,
  projectLabel as buildPageProjectLabel,
  shortSHA,
} from "./BuildDetailPage.helpers";

const TYPE_LABELS: Record<ArtifactType, string> = {
  docker_image: "Docker image",
  npm_package: "npm package",
  generic: "Generic artifact",
  unknown: "Unknown",
};

function artifactTitle(artifact: ArtifactDetail): string {
  return formatArtifactTitle(artifact);
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

function tagKind(tag: VersionTag): "version" | "channel" {
  return tag.kind === "channel" ? "channel" : "version";
}

function isArtifactNotFoundError(error: unknown): boolean {
  return (
    error instanceof APIError &&
    (error.code === "artifact_not_found" || error.status === 404)
  );
}

function tagLabelList(tags: VersionTag[]) {
  if (tags.length === 0) {
    return <span className="subtle-text">None</span>;
  }
  return (
    <>
      {tags.map((tag) => (
        <span key={tag.id} className="version-tag-pill">
          {tag.version}
        </span>
      ))}
    </>
  );
}

function typeLabel(value: ArtifactType): string {
  return TYPE_LABELS[value] ?? value;
}

function safeExternalURL(value: string | null | undefined): string | null {
  const trimmed = value?.trim();
  if (!trimmed) {
    return null;
  }

  try {
    const parsed = new URL(trimmed);
    if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
      return null;
    }
    return parsed.toString();
  } catch {
    return null;
  }
}

function relatedArtifacts(
  artifacts: BuildArtifact[] | undefined,
  currentArtifactID: string,
): BuildArtifact[] {
  return (artifacts ?? []).filter(
    (artifact) => artifact.id !== currentArtifactID,
  );
}

export function ArtifactDetailPage() {
  const { id } = useParams<{ id: string }>();
  const { data, isLoading, error } = useQuery({
    queryKey: ["artifact", id],
    queryFn: () => getArtifact(id!),
    enabled: !!id,
  });
  const { data: build } = useQuery({
    queryKey: ["build", data?.build_id],
    queryFn: () => getBuild(data!.build_id),
    enabled: Boolean(data?.build_id),
  });
  const {
    data: buildArtifacts,
    error: buildArtifactsError,
    isLoading: buildArtifactsLoading,
  } = useQuery({
    queryKey: ["buildArtifacts", data?.build_id],
    queryFn: () => getBuildArtifacts(data!.build_id),
    enabled: Boolean(data?.build_id),
  });

  if (isLoading) {
    return <p>Loading artifact…</p>;
  }
  if (isArtifactNotFoundError(error)) {
    return <p className="error-text">Artifact not found.</p>;
  }
  if (error) {
    return (
      <p className="error-text">Failed to load artifact: {String(error)}</p>
    );
  }
  if (!data) {
    return <p className="error-text">Artifact not found.</p>;
  }

  const versionTags = (data.version_tags ?? []).filter(
    (tag) => tagKind(tag) === "version",
  );
  const channelTags = (data.version_tags ?? []).filter(
    (tag) => tagKind(tag) === "channel",
  );
  const sourceRef = build ? buildSourceRefValue(build) : null;
  const primaryCommit = build ? buildPrimaryCommitValue(build) : null;
  const sourceRefHref = build ? buildGitHubRefURL(build, sourceRef) : null;
  const primaryCommitHref = build
    ? buildGitHubCommitURL(build, primaryCommit)
    : null;
  const repositoryText =
    build?.repository_url?.trim() ||
    build?.source?.repository_url?.trim() ||
    null;
  const repositoryURL = safeExternalURL(repositoryText);
  const siblingArtifacts = relatedArtifacts(buildArtifacts, data.id);

  return (
    <>
      <div className="artifact-detail-back-links subtle-text">
        <Link to="/artifacts">← Back to artifacts</Link>
        <span aria-hidden="true">·</span>
        <Link to={`/builds/${data.build_id}`}>View producing build</Link>
      </div>

      <section className="detail-panel artifact-detail-header">
        <div>
          <h2>{artifactTitle(data)}</h2>
          {artifactHeaderPath(data) && (
            <p className="subtle-text artifact-mono">
              {artifactHeaderPath(data)}
            </p>
          )}
          <div className="artifact-card-meta">
            <span className="artifact-type-pill">
              {typeLabel(data.artifact_type)}
            </span>
            <span className="artifact-secondary-pill">{stepLabel(data)}</span>
            <span className="artifact-secondary-pill">
              {build ? buildPageLabel(build) : buildLabel(data)}
            </span>
          </div>
        </div>
        <div className="artifact-actions">
          <Link className="secondary-button" to={`/builds/${data.build_id}`}>
            Open build
          </Link>
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
        <h3>Identity</h3>
        <div className="artifact-detail-grid">
          <div>
            <strong>Name</strong>
            <span>{artifactTitle(data)}</span>
          </div>
          <div>
            <strong>Type</strong>
            <span>{typeLabel(data.artifact_type)}</span>
          </div>
          <div>
            <strong>Version labels</strong>
            <div className="version-tag-list" aria-label="Artifact versions">
              {tagLabelList(versionTags)}
            </div>
          </div>
          <div>
            <strong>Channels</strong>
            <div className="version-tag-list" aria-label="Artifact channels">
              {tagLabelList(channelTags)}
            </div>
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
            <strong>Storage</strong>
            <span>{data.storage_provider}</span>
          </div>
          <div>
            <strong>Path</strong>
            <span className="artifact-mono">{data.path}</span>
          </div>
          <div>
            <strong>Content Type</strong>
            <span>{data.content_type ?? "—"}</span>
          </div>
          <div className="artifact-version-meta-full">
            <strong>Digest</strong>
            <span
              className="artifact-mono"
              title={data.checksum_sha256 ?? undefined}
            >
              {data.checksum_sha256
                ? formatChecksumDisplay(data.checksum_sha256)
                : "—"}
            </span>
          </div>
        </div>
      </section>

      <section className="detail-panel">
        <h3>Produced By</h3>
        <div className="artifact-detail-grid">
          <div>
            <strong>Project</strong>
            <span>
              {build ? (
                <Link to={`/projects/${build.project_id}`}>
                  {buildPageProjectLabel(build)}
                </Link>
              ) : (
                projectLabel(data)
              )}
            </span>
          </div>
          <div>
            <strong>Job</strong>
            <span>
              {data.job_id ? (
                <Link to={`/jobs/${data.job_id}`}>
                  {build ? buildPageJobLabel(build) : jobLabel(data)}
                </Link>
              ) : (
                "—"
              )}
            </span>
          </div>
          <div>
            <strong>Build</strong>
            <span>
              <Link to={`/builds/${data.build_id}`}>
                {build ? buildPageLabel(build) : buildLabel(data)}
              </Link>
            </span>
          </div>
          <div>
            <strong>Status</strong>
            <span>
              <StatusBadge status={data.build_status} />
            </span>
          </div>
          <div>
            <strong>Step</strong>
            <span>{stepLabel(data)}</span>
          </div>
        </div>
      </section>

      <section className="detail-panel">
        <h3>Source Provenance</h3>
        <div className="artifact-detail-grid">
          <div>
            <strong>Repository</strong>
            <span>
              {repositoryURL ? (
                <a href={repositoryURL}>{repositoryURL}</a>
              ) : repositoryText ? (
                repositoryText
              ) : (
                "—"
              )}
            </span>
          </div>
          <div>
            <strong>Ref</strong>
            <span>
              {sourceRef ? (
                sourceRefHref ? (
                  <a href={sourceRefHref}>{sourceRef}</a>
                ) : (
                  sourceRef
                )
              ) : (
                "—"
              )}
            </span>
          </div>
          <div>
            <strong>Commit</strong>
            <span className="artifact-mono">
              {primaryCommit ? (
                primaryCommitHref ? (
                  <a href={primaryCommitHref}>{shortSHA(primaryCommit)}</a>
                ) : (
                  shortSHA(primaryCommit)
                )
              ) : (
                "—"
              )}
            </span>
          </div>
          <div>
            <strong>Build trigger</strong>
            <span>{build?.trigger_kind ?? "—"}</span>
          </div>
        </div>
        {!build && !repositoryText && !sourceRef && !primaryCommit ? (
          <p className="subtle-text artifact-detail-section-note">
            Producing build provenance is not available yet.
          </p>
        ) : null}
      </section>

      <section className="detail-panel">
        <h3>Related Artifacts</h3>
        {buildArtifactsLoading ? (
          <p className="subtle-text">Loading related artifacts…</p>
        ) : buildArtifactsError ? (
          <p className="subtle-text">
            Related artifacts are unavailable: {String(buildArtifactsError)}
          </p>
        ) : siblingArtifacts.length === 0 ? (
          <p className="subtle-text">
            No other artifacts from this build were recorded.
          </p>
        ) : (
          <div className="job-latest-outputs-list">
            {siblingArtifacts.map((artifact) => (
              <article key={artifact.id} className="job-latest-output-item">
                <div className="job-latest-output-copy">
                  <Link to={`/artifacts/${artifact.id}`}>
                    {formatArtifactTitle(artifact)}
                  </Link>
                  {artifactSecondaryPath(artifact) ? (
                    <div className="subtle-text artifact-mono">
                      {artifactSecondaryPath(artifact)}
                    </div>
                  ) : null}
                  <div className="job-latest-output-meta subtle-text">
                    <span>{formatTime(artifact.created_at)}</span>
                    <span>{formatFileSize(artifact.size_bytes)}</span>
                    <span>
                      {artifact.checksum_sha256
                        ? formatChecksumDisplay(artifact.checksum_sha256)
                        : "No digest"}
                    </span>
                  </div>
                </div>
                <div className="artifact-actions job-latest-output-actions">
                  <Link to={`/artifacts/${artifact.id}`}>Open artifact</Link>
                  <a href={artifactDownloadURL(artifact.download_url_path)}>
                    Download
                  </a>
                </div>
              </article>
            ))}
          </div>
        )}
      </section>
    </>
  );
}
