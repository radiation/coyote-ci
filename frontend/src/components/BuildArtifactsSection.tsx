import { artifactDownloadURL } from "../api";
import { Link } from "react-router-dom";
import type { BuildArtifact, BuildStep } from "../types";
import {
  artifactSecondaryPath,
  artifactTitle,
  formatChecksumDisplay,
  formatFileSize,
} from "../utils/format";
import { formatTime } from "../utils/time";
import { VersionTagEditor } from "./VersionTagEditor";

interface Props {
  artifacts: BuildArtifact[];
  steps?: BuildStep[];
  isLoading: boolean;
  error: unknown;
  onAssignVersion?: (artifactID: string, version: string) => Promise<void>;
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
  items,
  onAssignVersion,
}: {
  items: BuildArtifact[];
  onAssignVersion?: (artifactID: string, version: string) => Promise<void>;
}) {
  return (
    <table className="table artifacts-table">
      <thead>
        <tr>
          <th>Artifact</th>
          <th>Size</th>
          <th>Checksum</th>
          <th>Version Tags</th>
          <th>Created</th>
          <th>
            <span className="sr-only">Actions</span>
          </th>
        </tr>
      </thead>
      <tbody>
        {sortArtifacts(items).map((item) => (
          <tr key={item.id}>
            <td className="artifact-path">
              <div className="artifact-catalog-primary">
                <Link to={`/artifacts/${item.id}`}>{artifactTitle(item)}</Link>
                {artifactSecondaryPath(item) && (
                  <div className="subtle-text artifact-mono">
                    {artifactSecondaryPath(item)}
                  </div>
                )}
              </div>
            </td>
            <td>{formatFileSize(item.size_bytes)}</td>
            <td className="artifact-mono artifact-checksum-cell">
              {item.checksum_sha256 ? (
                <span
                  className="artifact-checksum-value"
                  title={item.checksum_sha256}
                >
                  {formatChecksumDisplay(item.checksum_sha256)}
                </span>
              ) : (
                "—"
              )}
            </td>
            <td>
              <VersionTagEditor
                tags={item.version_tags ?? []}
                emptyText="No version tags yet."
                inputLabel={`artifact-version-${item.id}`}
                onAssign={
                  onAssignVersion
                    ? (version) => onAssignVersion(item.id, version)
                    : undefined
                }
              />
            </td>
            <td>{formatTime(item.created_at)}</td>
            <td>
              <div className="artifact-actions">
                <Link to={`/artifacts/${item.id}`}>Open</Link>
                <a href={artifactDownloadURL(item.download_url_path)}>
                  Download
                </a>
              </div>
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

export function BuildArtifactsSection({
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
      <p className="subtle-text">No artifacts were collected for this build.</p>
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
          <ArtifactTable items={shared} onAssignVersion={onAssignVersion} />
        </div>
      )}
      {stepEntries.map(([stepId, items]) => (
        <div key={stepId} className="artifact-group">
          <h4 className="artifact-group-label">{stepLabel(stepId, steps)}</h4>
          <ArtifactTable items={items} onAssignVersion={onAssignVersion} />
        </div>
      ))}
    </div>
  );
}
