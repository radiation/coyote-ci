import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { Link, useParams } from "react-router-dom";
import {
  artifactDownloadURL,
  createJobVersionTags,
  getArtifact,
  getBuild,
  getBuildArtifacts,
} from "../api";
import { VersionTagEditor } from "../components/VersionTagEditor";
import { StatusBadge } from "../components/StatusBadge";
import { APIError } from "../api/request";
import type { ArtifactDetail, BuildArtifact, VersionTag } from "../types";
import {
  artifactTypeLabel,
  artifactSecondaryPath,
  artifactTitle as formatArtifactTitle,
  formatChecksumDisplay,
  formatFileSize,
} from "../utils/format";
import { formatTime } from "../utils/time";
import {
  buildLabel as buildPageLabel,
  jobLabel as buildPageJobLabel,
  projectLabel as buildPageProjectLabel,
} from "./BuildDetailPage.helpers";
import {
  buildGitHubCommitURL,
  buildGitHubRefURL,
  buildPrimaryCommitValue,
  buildSourceRefValue,
  shortSHA,
} from "../utils/provenance";

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

function artifactLineageArtifactLabel(artifact: ArtifactDetail): string {
  const lineageName = artifact.lineage?.artifact_name?.trim() ?? "";
  if (lineageName) {
    return lineageName;
  }
  return artifactTitle(artifact);
}

function artifactLineageVersionLabel(artifact: ArtifactDetail): string | null {
  const lineageVersion = artifact.lineage?.versions?.find((value) =>
    value.trim(),
  );
  if (lineageVersion) {
    return lineageVersion;
  }
  const versionTag = (artifact.version_tags ?? []).find(
    (tag) => tagKind(tag) === "version" && tag.version.trim(),
  );
  return versionTag?.version ?? null;
}

function artifactLineageChannelLabel(artifact: ArtifactDetail): string | null {
  const lineageChannel = artifact.lineage?.channels?.find((value) =>
    value.trim(),
  );
  if (lineageChannel) {
    return lineageChannel;
  }
  const channelTag = (artifact.version_tags ?? []).find(
    (tag) => tagKind(tag) === "channel" && tag.version.trim(),
  );
  return channelTag?.version ?? null;
}

function artifactLineageSegments(
  artifact: ArtifactDetail,
  buildHref: string,
  primaryCommit: string | null,
  primaryCommitHref: string | null,
  sourceRef: string | null,
  sourceRefHref: string | null,
): Array<{ key: string; content: ReactNode }> {
  const segments: Array<{ key: string; content: ReactNode }> = [];
  if (primaryCommit) {
    segments.push({
      key: "commit",
      content: primaryCommitHref ? (
        <a href={primaryCommitHref}>Commit {shortSHA(primaryCommit)}</a>
      ) : (
        <span>Commit {shortSHA(primaryCommit)}</span>
      ),
    });
  } else if (sourceRef) {
    segments.push({
      key: "ref",
      content: sourceRefHref ? (
        <a href={sourceRefHref}>Ref {sourceRef}</a>
      ) : (
        <span>Ref {sourceRef}</span>
      ),
    });
  }
  segments.push({
    key: "build",
    content: <Link to={buildHref}>{buildLabel(artifact)}</Link>,
  });
  segments.push({
    key: "artifact",
    content: (
      <Link to={`/artifacts/${artifact.id}`}>
        {artifactLineageArtifactLabel(artifact)}
      </Link>
    ),
  });
  const versionLabelValue = artifactLineageVersionLabel(artifact);
  if (versionLabelValue) {
    segments.push({
      key: "version",
      content: <span>Version {versionLabelValue}</span>,
    });
  }
  const channelLabelValue = artifactLineageChannelLabel(artifact);
  if (channelLabelValue) {
    segments.push({
      key: "channel",
      content: <span>Channel {channelLabelValue}</span>,
    });
  }
  return segments;
}

export function ArtifactDetailPage() {
  const { id } = useParams<{ id: string }>();
  const queryClient = useQueryClient();
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
  const createVersionTagMutation = useMutation({
    mutationFn: ({
      jobID,
      version,
      kind,
      artifactID,
    }: {
      jobID: string;
      version: string;
      kind?: "version" | "channel";
      artifactID: string;
    }) =>
      createJobVersionTags(jobID, {
        version,
        kind,
        artifact_ids: [artifactID],
      }),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["artifact", id] }),
        queryClient.invalidateQueries({
          queryKey: ["buildArtifacts", data?.build_id],
        }),
        queryClient.invalidateQueries({ queryKey: ["artifactLogicalBrowse"] }),
        queryClient.invalidateQueries({ queryKey: ["artifactCatalog"] }),
      ]);
    },
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

  const artifactID = data.id;
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
  const tagJobID = data.job_id ?? build?.job_id ?? null;
  const lineageVersion = artifactLineageVersionLabel(data);
  const lineageChannel = artifactLineageChannelLabel(data);

  async function assignArtifactTag(
    value: string,
    kind?: "version" | "channel",
  ) {
    if (!tagJobID) {
      throw new Error("Artifact is not associated with a job.");
    }

    await createVersionTagMutation.mutateAsync({
      jobID: tagJobID,
      version: value,
      kind,
      artifactID,
    });
  }

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
              {artifactTypeLabel(data.artifact_type)}
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
        <h3>Lineage</h3>
        <div className="artifact-lineage-trail artifact-lineage-trail-detail">
          {artifactLineageSegments(
            data,
            `/builds/${data.build_id}`,
            primaryCommit,
            primaryCommitHref,
            sourceRef,
            sourceRefHref,
          ).map((segment, index) => (
            <span key={segment.key}>
              {index > 0 ? (
                <span className="artifact-lineage-separator" aria-hidden="true">
                  →
                </span>
              ) : null}
              {segment.content}
            </span>
          ))}
        </div>
        {!primaryCommit && !sourceRef && !lineageVersion && !lineageChannel ? (
          <p className="subtle-text artifact-detail-section-note">
            Version and source lineage are only partially available for this
            artifact.
          </p>
        ) : null}
      </section>

      <section className="detail-panel">
        <h3>Identity</h3>
        <div className="artifact-detail-grid">
          <div>
            <strong>Name</strong>
            <span>{artifactTitle(data)}</span>
          </div>
          <div>
            <strong>Type</strong>
            <span>{artifactTypeLabel(data.artifact_type)}</span>
          </div>
          <div>
            <strong>Versions</strong>
            <VersionTagEditor
              tags={versionTags}
              emptyText="No versions yet."
              inputLabel={`artifact-detail-version-${artifactID}`}
              submitLabel="Assign version"
              requiredMessage="Version is required."
              onAssign={
                tagJobID
                  ? (value) => assignArtifactTag(value, "version")
                  : undefined
              }
            />
          </div>
          <div>
            <strong>Channels</strong>
            <VersionTagEditor
              tags={channelTags}
              emptyText="No channels yet."
              inputLabel={`artifact-detail-channel-${artifactID}`}
              submitLabel="Assign channel"
              placeholder="stable"
              requiredMessage="Channel is required."
              onAssign={
                tagJobID
                  ? (value) => assignArtifactTag(value, "channel")
                  : undefined
              }
            />
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
        <h3>Produced by</h3>
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
        <h3>Source provenance</h3>
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
            <strong>Source ref</strong>
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
        <h3>Related artifacts</h3>
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
