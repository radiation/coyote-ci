import { artifactDownloadURL } from "../api";
import type { BuildArtifact, BuildStep } from "../types";
import { formatFileSize } from "../utils/format";
import { formatTime } from "../utils/time";

interface Props {
  artifacts: BuildArtifact[];
  steps?: BuildStep[];
  isLoading: boolean;
  error: unknown;
}

function stepLabel(stepId: string, steps: BuildStep[] | undefined): string {
  if (steps) {
    const step = steps.find((s) => s.id === stepId);
    if (step) return `Step ${step.step_index}: ${step.name}`;
  }
  return `Step ${stepId.slice(0, 8)}…`;
}

function ArtifactTable({ items }: { items: BuildArtifact[] }) {
  return (
    <table className="table artifacts-table">
      <thead>
        <tr>
          <th>Path</th>
          <th>Size</th>
          <th>Created</th>
          <th></th>
        </tr>
      </thead>
      <tbody>
        {items.map((item) => (
          <tr key={item.id}>
            <td className="artifact-path">{item.path}</td>
            <td>{formatFileSize(item.size_bytes)}</td>
            <td>{formatTime(item.created_at)}</td>
            <td>
              <a href={artifactDownloadURL(item.download_url_path)}>Download</a>
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
  const byStep = new Map<string, BuildArtifact[]>();
  for (const a of artifacts) {
    if (a.step_id) {
      const list = byStep.get(a.step_id) ?? [];
      list.push(a);
      byStep.set(a.step_id, list);
    }
  }

  // Sort step groups by first artifact's created_at for stable ordering.
  const stepEntries = [...byStep.entries()].sort((a, b) => {
    const aTime = a[1][0]?.created_at ?? "";
    const bTime = b[1][0]?.created_at ?? "";
    return aTime.localeCompare(bTime);
  });

  return (
    <div className="artifacts-section">
      {shared.length > 0 && (
        <div className="artifact-group">
          <h4 className="artifact-group-label">Build-level</h4>
          <ArtifactTable items={shared} />
        </div>
      )}
      {stepEntries.map(([stepId, items]) => (
        <div key={stepId} className="artifact-group">
          <h4 className="artifact-group-label">{stepLabel(stepId, steps)}</h4>
          <ArtifactTable items={items} />
        </div>
      ))}
    </div>
  );
}
