import { useCallback } from "react";
import {
  keepPreviousData,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { Link, useSearchParams } from "react-router-dom";
import { createJobVersionTags, listArtifacts, listProjects } from "../api";
import { ArtifactBrowser } from "../components/ArtifactBrowser";
import type { ArtifactBrowseVersion, ArtifactType } from "../types";

const DEFAULT_LOGICAL_PAGE_SIZE = 20;

const ARTIFACT_TYPE_OPTIONS: Array<{
  value: "" | ArtifactType;
  label: string;
}> = [
  { value: "", label: "All types" },
  { value: "docker_image", label: "Docker image" },
  { value: "npm_package", label: "npm package" },
  { value: "generic", label: "Generic artifact" },
  { value: "unknown", label: "Unknown" },
];

function parsePositiveInt(value: string | null, fallback: number): number {
  if (!value) {
    return fallback;
  }
  const parsed = Number.parseInt(value, 10);
  if (!Number.isFinite(parsed) || parsed < 1) {
    return fallback;
  }
  return parsed;
}

function buildCanonicalSearchParams(params: URLSearchParams) {
  const nextParams = new URLSearchParams();

  const query = params.get("q")?.trim() ?? "";
  if (query) {
    nextParams.set("q", query);
  }

  const type = params.get("type")?.trim() ?? "";
  if (type) {
    nextParams.set("type", type);
  }

  const projectID = params.get("project_id")?.trim() ?? "";
  if (projectID) {
    nextParams.set("project_id", projectID);
  }

  const page = parsePositiveInt(params.get("page"), 1);
  if (page > 1) {
    nextParams.set("page", String(page));
  }

  return nextParams;
}

export function ArtifactLogicalBrowserPage() {
  const [searchParams, setSearchParams] = useSearchParams();
  const queryClient = useQueryClient();

  const search = searchParams.get("q") ?? "";
  const type = searchParams.get("type")?.trim() ?? "";
  const projectID = searchParams.get("project_id")?.trim() ?? "";
  const pageIndex = parsePositiveInt(searchParams.get("page"), 1) - 1;
  const trimmedSearch = search.trim();
  const hasActiveFilters = Boolean(trimmedSearch || type || projectID);

  const updateSearchParams = useCallback(
    (mutate: (nextParams: URLSearchParams) => void) => {
      const draftParams = new URLSearchParams(searchParams);
      mutate(draftParams);
      const nextParams = buildCanonicalSearchParams(draftParams);

      if (nextParams.toString() !== searchParams.toString()) {
        setSearchParams(nextParams, { replace: true });
      }
    },
    [searchParams, setSearchParams],
  );

  const {
    data: artifactResults = [],
    isLoading,
    isFetching,
    error,
  } = useQuery({
    queryKey: [
      "artifactLogicalBrowse",
      trimmedSearch,
      type,
      projectID,
      pageIndex,
    ],
    queryFn: () =>
      listArtifacts({
        q: trimmedSearch,
        type: type || undefined,
        project_id: projectID || undefined,
        limit: DEFAULT_LOGICAL_PAGE_SIZE + 1,
        offset: pageIndex * DEFAULT_LOGICAL_PAGE_SIZE,
      }),
    placeholderData: keepPreviousData,
  });

  const { data: projects = [] } = useQuery({
    queryKey: ["projects"],
    queryFn: () => listProjects(),
  });

  const createVersionTagMutation = useMutation({
    mutationFn: ({
      jobID,
      version,
      artifactID,
    }: {
      jobID: string;
      version: string;
      artifactID: string;
    }) =>
      createJobVersionTags(jobID, {
        version,
        artifact_ids: [artifactID],
      }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: ["artifactLogicalBrowse"],
      });
    },
  });

  const artifacts = artifactResults.slice(0, DEFAULT_LOGICAL_PAGE_SIZE);
  const hasNextPage = artifactResults.length > DEFAULT_LOGICAL_PAGE_SIZE;

  function handleSearchChange(value: string) {
    updateSearchParams((nextParams) => {
      if (value) {
        nextParams.set("q", value);
      } else {
        nextParams.delete("q");
      }
      nextParams.delete("page");
    });
  }

  function handleTypeChange(value: string) {
    updateSearchParams((nextParams) => {
      if (value) {
        nextParams.set("type", value);
      } else {
        nextParams.delete("type");
      }
      nextParams.delete("page");
    });
  }

  function handleProjectChange(value: string) {
    updateSearchParams((nextParams) => {
      if (value) {
        nextParams.set("project_id", value);
      } else {
        nextParams.delete("project_id");
      }
      nextParams.delete("page");
    });
  }

  function handleClearFilters() {
    setSearchParams(new URLSearchParams(), { replace: true });
  }

  function goToPage(nextPage: number) {
    const safePage = Math.max(1, nextPage);
    updateSearchParams((nextParams) => {
      if (safePage === 1) {
        nextParams.delete("page");
      } else {
        nextParams.set("page", String(safePage));
      }
    });
  }

  async function assignArtifactVersion(
    version: ArtifactBrowseVersion,
    releaseVersion: string,
  ) {
    if (!version.job_id) {
      throw new Error("Artifact version is not associated with a job.");
    }
    await createVersionTagMutation.mutateAsync({
      jobID: version.job_id,
      version: releaseVersion,
      artifactID: version.artifact_id,
    });
  }

  return (
    <>
      <div className="page-header-row">
        <div>
          <h2>Logical Artifact Browser</h2>
          <p className="subtle-text">
            Grouped logical artifacts and their published versions. Use this
            view to inspect version history and assign version tags.
            {isFetching && !isLoading ? " Updating…" : ""}
          </p>
        </div>
        <Link className="secondary-button" to="/artifacts">
          Open persisted catalog
        </Link>
      </div>

      <section
        className="artifact-filters-panel"
        aria-label="Logical artifact filters"
      >
        <div className="artifact-filter-toolbar">
          <div>
            <h3>Logical Artifact Browser</h3>
            <p className="subtle-text">
              {hasActiveFilters
                ? "Filtered grouped artifact view"
                : "Grouped logical artifacts"}
            </p>
          </div>
          <button
            type="button"
            className="secondary-button"
            onClick={handleClearFilters}
            disabled={!hasActiveFilters || isLoading}
          >
            Clear filters
          </button>
        </div>
        <label className="artifact-filter-field">
          <span>Search artifacts</span>
          <input
            type="search"
            value={search}
            onChange={(event) => handleSearchChange(event.target.value)}
            placeholder="Path, project, job, or version tag"
          />
        </label>
        <label className="artifact-filter-field artifact-filter-select">
          <span>Type</span>
          <select
            value={type}
            onChange={(event) => handleTypeChange(event.target.value)}
          >
            {ARTIFACT_TYPE_OPTIONS.map((option) => (
              <option key={option.label} value={option.value}>
                {option.label}
              </option>
            ))}
          </select>
        </label>
        <label className="artifact-filter-field artifact-filter-select">
          <span>Project</span>
          <select
            value={projectID}
            onChange={(event) => handleProjectChange(event.target.value)}
          >
            <option value="">All projects</option>
            {projects.map((project) => (
              <option key={project.id} value={project.id}>
                {project.name} ({project.slug})
              </option>
            ))}
          </select>
        </label>
      </section>

      <section
        className="artifact-pagination-bar"
        aria-label="Logical artifact pagination"
      >
        <p className="subtle-text artifact-pagination-status">
          {artifacts.length > 0
            ? `Showing ${pageIndex * DEFAULT_LOGICAL_PAGE_SIZE + 1}-${pageIndex * DEFAULT_LOGICAL_PAGE_SIZE + artifacts.length}${hasNextPage ? "; more available" : ""}`
            : pageIndex > 0
              ? `No artifacts on page ${pageIndex + 1}`
              : hasActiveFilters
                ? "No matching artifacts"
                : "No artifacts yet"}
        </p>
        <div className="artifact-pagination-actions">
          <button
            type="button"
            className="secondary-button"
            onClick={() => goToPage(pageIndex)}
            disabled={pageIndex === 0 || isFetching}
          >
            Previous
          </button>
          <span className="artifact-pagination-page">Page {pageIndex + 1}</span>
          <button
            type="button"
            className="secondary-button"
            onClick={() => goToPage(pageIndex + 2)}
            disabled={!hasNextPage || isFetching}
          >
            Next
          </button>
        </div>
      </section>

      <ArtifactBrowser
        artifacts={artifacts}
        isLoading={isLoading}
        error={error}
        hasActiveFilters={hasActiveFilters}
        pageIndex={pageIndex}
        onAssignVersion={assignArtifactVersion}
      />
    </>
  );
}
