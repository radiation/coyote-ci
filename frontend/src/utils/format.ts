import type { ArtifactType } from "../types";

const UNITS = ["B", "KB", "MB", "GB", "TB"] as const;

type ArtifactIdentity = {
  name?: string | null;
  path: string;
};

const ARTIFACT_TYPE_LABELS: Record<ArtifactType, string> = {
  docker_image: "Docker image",
  npm_package: "npm package",
  generic: "Generic artifact",
  unknown: "Unknown",
};

/** Format a byte count as a human-readable size string (e.g. "1.2 MB"). */
export function formatFileSize(bytes: number): string {
  if (bytes === 0) return "0 B";
  const i = Math.min(
    Math.floor(Math.log(bytes) / Math.log(1024)),
    UNITS.length - 1,
  );
  const value = bytes / 1024 ** i;
  return `${i === 0 ? value : value.toFixed(1)} ${UNITS[i]}`;
}

export function artifactTitle(artifact: ArtifactIdentity): string {
  return artifact.name?.trim() || artifact.path;
}

export function artifactSecondaryPath(
  artifact: ArtifactIdentity,
): string | null {
  const trimmedName = artifact.name?.trim() ?? "";
  if (!trimmedName || trimmedName === artifact.path) {
    return null;
  }
  return artifact.path;
}

export function artifactTypeLabel(type: ArtifactType): string {
  return ARTIFACT_TYPE_LABELS[type] ?? ARTIFACT_TYPE_LABELS.unknown;
}

export function formatChecksumDisplay(checksum: string): string {
  const trimmed = checksum.trim();
  if (trimmed.length <= 24) {
    return trimmed;
  }
  return `${trimmed.slice(0, 12)}…${trimmed.slice(-8)}`;
}
