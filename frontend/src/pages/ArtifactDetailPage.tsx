import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Link, useParams } from "react-router-dom";
import { createJobVersionTags, getArtifact, artifactDownloadURL } from "../api";
import { StatusBadge } from "../components/StatusBadge";
import { APIError } from "../api/request";
import type { ArtifactDetail, VersionTag } from "../types";
import { formatFileSize } from "../utils/format";
import { formatTime } from "../utils/time";

function mergeVersionTags(
  existing: VersionTag[] | undefined,
  created: VersionTag[],
): VersionTag[] {
  const merged = [...(existing ?? [])];
  const seenIDs = new Set(merged.map((tag) => tag.id));

  for (const tag of created) {
    if (seenIDs.has(tag.id)) {
      continue;
    }
    merged.push(tag);
    seenIDs.add(tag.id);
  }

  return merged;
}

function formatVersionTagError(error: unknown): Error {
  const message = error instanceof Error ? error.message : String(error);
  return new Error(message.replace(/^API\s+\d+:\s*/, ""));
}

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

export function ArtifactDetailPage() {
  const { id } = useParams<{ id: string }>();
  const queryClient = useQueryClient();
  const [labelValue, setLabelValue] = useState("");
  const [labelKind, setLabelKind] = useState<"version" | "channel">("version");
  const [submitError, setSubmitError] = useState<string | null>(null);
  const { data, isLoading, error } = useQuery({
    queryKey: ["artifact", id],
    queryFn: () => getArtifact(id!),
    enabled: !!id,
  });

  const createVersionTagMutation = useMutation({
    mutationFn: async ({
      version,
      kind,
    }: {
      version: string;
      kind: "version" | "channel";
    }) => {
      if (!id) {
        throw new Error("Artifact ID is required.");
      }
      if (!data?.job_id) {
        throw new Error("Artifact is not associated with a job.");
      }
      return createJobVersionTags(data.job_id, {
        kind,
        version,
        artifact_ids: [id],
      });
    },
    onSuccess: (createdTags) => {
      if (!id) {
        return;
      }
      queryClient.setQueryData<ArtifactDetail | undefined>(
        ["artifact", id],
        (current) => {
          if (!current) {
            return current;
          }
          const artifactTags = createdTags.filter(
            (tag) => tag.artifact_id === current.id,
          );
          if (artifactTags.length === 0) {
            return current;
          }
          return {
            ...current,
            version_tags: mergeVersionTags(current.version_tags, artifactTags),
          };
        },
      );
      setLabelValue("");
      setSubmitError(null);
    },
  });

  async function handleAssignVersionTag() {
    const trimmed = labelValue.trim();
    if (!trimmed) {
      setSubmitError("Label is required.");
      return;
    }
    try {
      setSubmitError(null);
      await createVersionTagMutation.mutateAsync({
        version: trimmed,
        kind: labelKind,
      });
    } catch (mutationError) {
      setSubmitError(formatVersionTagError(mutationError).message);
    }
  }

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

      <section className="detail-panel">
        <h3>Versions / Channels</h3>
        <p className="subtle-text">
          Versions are immutable releases. Channels are movable aliases such as
          latest or prod.
        </p>
        <div className="artifact-detail-grid">
          <div>
            <strong>Current Versions</strong>
            <div className="version-tag-list" aria-label="Artifact versions">
              {tagLabelList(versionTags)}
            </div>
          </div>
          <div>
            <strong>Current Channels</strong>
            <div className="version-tag-list" aria-label="Artifact channels">
              {tagLabelList(channelTags)}
            </div>
          </div>
        </div>
        {data.job_id && (
          <form
            className="version-tag-form"
            onSubmit={(event) => {
              event.preventDefault();
              void handleAssignVersionTag();
            }}
          >
            <label
              className="sr-only"
              htmlFor={`artifact-detail-kind-${data.id}`}
            >
              Artifact label kind
            </label>
            <select
              id={`artifact-detail-kind-${data.id}`}
              value={labelKind}
              onChange={(event) =>
                setLabelKind(event.target.value as "version" | "channel")
              }
              disabled={createVersionTagMutation.isPending}
            >
              <option value="version">Version</option>
              <option value="channel">Channel</option>
            </select>
            <label
              className="sr-only"
              htmlFor={`artifact-detail-label-${data.id}`}
            >
              Artifact label
            </label>
            <input
              id={`artifact-detail-label-${data.id}`}
              value={labelValue}
              onChange={(event) => setLabelValue(event.target.value)}
              placeholder={labelKind === "channel" ? "prod" : "1.2.3"}
              disabled={createVersionTagMutation.isPending}
            />
            <button type="submit" disabled={createVersionTagMutation.isPending}>
              {createVersionTagMutation.isPending ? "Saving…" : "Assign label"}
            </button>
          </form>
        )}
        {submitError && <p className="error-text">{submitError}</p>}
        {!data.job_id && (
          <p className="subtle-text">
            This artifact is not associated with a job, so new versions or
            channels cannot be assigned.
          </p>
        )}
      </section>
    </>
  );
}
