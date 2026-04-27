import { useDeferredValue, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createJobVersionTags, listArtifacts } from "../api";
import { ArtifactBrowser } from "../components/ArtifactBrowser";
import type { ArtifactBrowseVersion, ArtifactType } from "../types";
import { formatTime } from "../utils/time";

const ARTIFACT_TYPE_OPTIONS: Array<{
  label: string;
  value: ArtifactType | "";
}> = [
  { label: "All types", value: "" },
  { label: "Docker image", value: "docker_image" },
  { label: "npm package", value: "npm_package" },
  { label: "Generic artifact", value: "generic" },
  { label: "Unknown", value: "unknown" },
];

export function ArtifactsPage() {
  const queryClient = useQueryClient();
  const [search, setSearch] = useState("");
  const [typeFilter, setTypeFilter] = useState<ArtifactType | "">("");
  const deferredSearch = useDeferredValue(search.trim());

  const {
    data: artifacts,
    isLoading,
    error,
    dataUpdatedAt,
  } = useQuery({
    queryKey: ["artifacts", deferredSearch, typeFilter],
    queryFn: () =>
      listArtifacts({
        q: deferredSearch,
        type: typeFilter,
      }),
  });

  const createVersionTagMutation = useMutation({
    mutationFn: ({
      version,
      artifact,
    }: {
      version: string;
      artifact: ArtifactBrowseVersion;
    }) => {
      if (!artifact.job_id) {
        throw new Error("Artifact version is not associated with a job.");
      }
      return createJobVersionTags(artifact.job_id, {
        version,
        artifact_ids: [artifact.artifact_id],
      });
    },
    onSuccess: async (_data, variables) => {
      await queryClient.invalidateQueries({ queryKey: ["artifacts"] });
      await queryClient.invalidateQueries({
        queryKey: ["buildArtifacts", variables.artifact.build_id],
      });
    },
  });

  async function assignArtifactVersion(
    artifact: ArtifactBrowseVersion,
    version: string,
  ) {
    await createVersionTagMutation.mutateAsync({ artifact, version });
  }

  return (
    <>
      <div className="page-header-row">
        <div>
          <h2>Artifacts</h2>
          <p className="subtle-text">
            Browse logical artifacts, inspect their published versions, and add
            immutable version tags without leaving the repository view. Last
            updated:{" "}
            {dataUpdatedAt > 0
              ? formatTime(new Date(dataUpdatedAt).toISOString())
              : "—"}
          </p>
        </div>
      </div>

      <section className="artifact-filters-panel">
        <label className="artifact-filter-field">
          <span>Search artifacts</span>
          <input
            type="search"
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder="Search by path, project, job, or version"
          />
        </label>
        <label className="artifact-filter-field artifact-filter-select">
          <span>Type</span>
          <select
            value={typeFilter}
            onChange={(event) =>
              setTypeFilter(event.target.value as ArtifactType | "")
            }
          >
            {ARTIFACT_TYPE_OPTIONS.map((option) => (
              <option key={option.value || "all"} value={option.value}>
                {option.label}
              </option>
            ))}
          </select>
        </label>
      </section>

      <ArtifactBrowser
        artifacts={artifacts ?? []}
        isLoading={isLoading}
        error={error}
        onAssignVersion={assignArtifactVersion}
      />
    </>
  );
}
