import { useCallback } from "react";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { Link, useSearchParams } from "react-router-dom";
import { artifactDownloadURL, listArtifactCatalog, listProjects } from "../api";
import { StatusBadge } from "../components/StatusBadge";
import type { ArtifactCatalogItem, ArtifactType } from "../types";
import {
  artifactSecondaryPath,
  artifactTitle,
  formatChecksumDisplay,
  formatFileSize,
} from "../utils/format";
import { formatTime } from "../utils/time";

const DEFAULT_ARTIFACTS_PAGE_SIZE = 20;
const ARTIFACTS_PAGE_SIZE_OPTIONS = [20, 50, 100];

const TYPE_LABELS: Record<ArtifactType, string> = {
  docker_image: "Docker image",
  npm_package: "npm package",
  generic: "Generic artifact",
  unknown: "Unknown",
};

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

function parsePageSizeParam(value: string | null): number {
  const parsed = parsePositiveInt(value, DEFAULT_ARTIFACTS_PAGE_SIZE);
  if (ARTIFACTS_PAGE_SIZE_OPTIONS.includes(parsed)) {
    return parsed;
  }
  return DEFAULT_ARTIFACTS_PAGE_SIZE;
}

function buildCanonicalSearchParams(params: URLSearchParams) {
  const nextParams = new URLSearchParams();

  const query = params.get("q")?.trim() ?? "";
  if (query) {
    nextParams.set("q", query);
  }

  const projectID = params.get("project_id")?.trim() ?? "";
  if (projectID) {
    nextParams.set("project_id", projectID);
  }

  const jobID = params.get("job_id")?.trim() ?? "";
  if (jobID) {
    nextParams.set("job_id", jobID);
  }

  const buildID = params.get("build_id")?.trim() ?? "";
  if (buildID) {
    nextParams.set("build_id", buildID);
  }

  const page = parsePositiveInt(params.get("page"), 1);
  if (page > 1) {
    nextParams.set("page", String(page));
  }

  const size = parsePageSizeParam(params.get("pageSize"));
  if (size !== DEFAULT_ARTIFACTS_PAGE_SIZE) {
    nextParams.set("pageSize", String(size));
  }

  return nextParams;
}

function buildLabel(artifact: ArtifactCatalogItem): string {
  if (artifact.build_number > 0) {
    return `Build #${artifact.build_number}`;
  }
  return `Build ${artifact.build_id.slice(0, 8)}…`;
}

function jobLabel(artifact: ArtifactCatalogItem): string {
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

function stepLabel(artifact: ArtifactCatalogItem): string {
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

function projectLabel(artifact: ArtifactCatalogItem): string {
  const name = artifact.project_name?.trim() ?? "";
  const slug = artifact.project_slug?.trim() ?? "";
  return name || slug || artifact.project_id;
}

function typeLabel(artifact: ArtifactCatalogItem): string {
  return TYPE_LABELS[artifact.artifact_type] ?? artifact.artifact_type;
}

export function ArtifactsPage() {
  const [searchParams, setSearchParams] = useSearchParams();
  const search = searchParams.get("q") ?? "";
  const projectID = searchParams.get("project_id")?.trim() ?? "";
  const jobID = searchParams.get("job_id")?.trim() ?? "";
  const buildID = searchParams.get("build_id")?.trim() ?? "";
  const pageIndex = parsePositiveInt(searchParams.get("page"), 1) - 1;
  const pageSize = parsePageSizeParam(searchParams.get("pageSize"));
  const trimmedSearch = search.trim();
  const hasActiveFilters = Boolean(
    trimmedSearch || projectID || jobID || buildID,
  );
  const activeFilterCount =
    (trimmedSearch ? 1 : 0) +
    (projectID ? 1 : 0) +
    (jobID ? 1 : 0) +
    (buildID ? 1 : 0);

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
    data: artifactResults,
    isLoading,
    isFetching,
    error,
    dataUpdatedAt,
  } = useQuery({
    queryKey: [
      "artifactCatalog",
      trimmedSearch,
      projectID,
      jobID,
      buildID,
      pageIndex,
      pageSize,
    ],
    queryFn: () =>
      listArtifactCatalog({
        q: trimmedSearch,
        project_id: projectID || undefined,
        job_id: jobID || undefined,
        build_id: buildID || undefined,
        limit: pageSize + 1,
        offset: pageIndex * pageSize,
      }),
    placeholderData: keepPreviousData,
  });
  const { data: projects = [] } = useQuery({
    queryKey: ["projects"],
    queryFn: () => listProjects(),
  });

  const artifacts = artifactResults?.slice(0, pageSize) ?? [];
  const hasNextPage = (artifactResults?.length ?? 0) > pageSize;
  const pageStart = pageIndex * pageSize + 1;
  const pageEnd = pageIndex * pageSize + artifacts.length;

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

  function handleJobChange(value: string) {
    updateSearchParams((nextParams) => {
      if (value) {
        nextParams.set("job_id", value);
      } else {
        nextParams.delete("job_id");
      }
      nextParams.delete("page");
    });
  }

  function handleBuildChange(value: string) {
    updateSearchParams((nextParams) => {
      if (value) {
        nextParams.set("build_id", value);
      } else {
        nextParams.delete("build_id");
      }
      nextParams.delete("page");
    });
  }

  function handlePageSizeChange(value: number) {
    updateSearchParams((nextParams) => {
      if (value === DEFAULT_ARTIFACTS_PAGE_SIZE) {
        nextParams.delete("pageSize");
      } else {
        nextParams.set("pageSize", String(value));
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

  return (
    <>
      <div className="page-header-row">
        <div>
          <h2>Artifacts</h2>
          <p className="subtle-text">
            Persisted artifact catalog.
            {isFetching && !isLoading ? " Updating…" : ""}
          </p>
          <p className="subtle-text">
            <Link to="/artifacts/logical">Open logical browser</Link> for
            grouped versions and tags.
          </p>
          <p className="subtle-text">
            Updated{" "}
            {dataUpdatedAt > 0
              ? formatTime(new Date(dataUpdatedAt).toISOString())
              : "—"}
          </p>
        </div>
      </div>

      <section className="artifact-filters-panel" aria-label="Artifact filters">
        <div className="artifact-filter-toolbar">
          <div>
            <h3>Artifact Catalog</h3>
            <p className="subtle-text">
              {activeFilterCount > 0
                ? `${activeFilterCount} active filter${activeFilterCount === 1 ? "" : "s"}`
                : "All artifacts"}
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
            placeholder="Name, path, build, or artifact id"
          />
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
        <label className="artifact-filter-field">
          <span>Job ID</span>
          <input
            type="search"
            value={jobID}
            onChange={(event) => handleJobChange(event.target.value)}
            placeholder="job-123"
          />
        </label>
        <label className="artifact-filter-field">
          <span>Build ID</span>
          <input
            type="search"
            value={buildID}
            onChange={(event) => handleBuildChange(event.target.value)}
            placeholder="build-123"
          />
        </label>
      </section>

      <section
        className="artifact-pagination-bar"
        aria-label="Artifact pagination"
      >
        <p className="subtle-text artifact-pagination-status">
          {artifacts.length > 0
            ? `Showing ${pageStart}-${pageEnd}${hasNextPage ? "; more available" : ""}`
            : pageIndex > 0
              ? `No artifacts on page ${pageIndex + 1}`
              : hasActiveFilters
                ? "No matching artifacts"
                : "No artifacts yet"}
        </p>
        <div className="artifact-pagination-actions">
          <label className="artifact-pagination-field">
            <span>Items per page</span>
            <select
              value={pageSize}
              onChange={(event) =>
                handlePageSizeChange(Number.parseInt(event.target.value, 10))
              }
              disabled={isLoading}
            >
              {ARTIFACTS_PAGE_SIZE_OPTIONS.map((option) => (
                <option key={option} value={option}>
                  {option}
                </option>
              ))}
            </select>
          </label>
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

      {isLoading ? (
        <p>Loading artifacts…</p>
      ) : error ? (
        <div className="empty-state artifact-empty-state artifact-error-state">
          <p className="error-text">
            Failed to load artifacts: {String(error)}
          </p>
        </div>
      ) : artifacts.length === 0 ? (
        <div className="empty-state artifact-empty-state">
          <p className="empty">
            {pageIndex > 0
              ? "No artifacts on this page."
              : hasActiveFilters
                ? "No artifacts matched the current filters."
                : "No artifacts have been published yet."}
          </p>
          <p className="subtle-text">
            {hasActiveFilters
              ? "Adjust the search or clear filters to broaden the repository view."
              : "Published build outputs will appear here with lineage back to their producing builds."}
          </p>
        </div>
      ) : (
        <section className="artifacts-catalog-panel">
          <div className="artifact-build-list">
            {artifacts.map((artifact) => (
              <article key={artifact.id} className="artifact-build-card">
                <div className="artifact-build-card-header">
                  <div className="artifact-catalog-primary">
                    <div className="artifact-card-kicker">
                      <span className="artifact-type-pill">
                        {typeLabel(artifact)}
                      </span>
                      <span className="artifact-secondary-pill">
                        {stepLabel(artifact)}
                      </span>
                      <span className="artifact-secondary-pill">
                        Project {projectLabel(artifact)}
                      </span>
                      {artifact.job_id ? (
                        <span className="artifact-secondary-pill">
                          Job {jobLabel(artifact)}
                        </span>
                      ) : null}
                    </div>
                    <Link
                      className="artifact-build-link"
                      to={`/artifacts/${artifact.id}`}
                    >
                      {artifactTitle(artifact)}
                    </Link>
                    {artifactSecondaryPath(artifact) ? (
                      <span className="subtle-text artifact-mono">
                        {artifactSecondaryPath(artifact)}
                      </span>
                    ) : null}
                  </div>
                  <div className="artifact-card-summary">
                    <span className="artifact-summary-primary">
                      <Link to={`/builds/${artifact.build_id}`}>
                        {buildLabel(artifact)}
                      </Link>
                    </span>
                    <StatusBadge status={artifact.build_status} />
                    <span>{formatTime(artifact.created_at)}</span>
                    <span>{formatFileSize(artifact.size_bytes)}</span>
                  </div>
                </div>

                <div className="artifact-detail-grid artifact-build-card-grid">
                  <div>
                    <strong>Artifact path</strong>
                    <span className="artifact-mono">{artifact.path}</span>
                  </div>
                  <div>
                    <strong>Build</strong>
                    <span>
                      <Link to={`/builds/${artifact.build_id}`}>
                        {buildLabel(artifact)}
                      </Link>
                    </span>
                  </div>
                  <div>
                    <strong>Build status</strong>
                    <span>
                      <StatusBadge status={artifact.build_status} />
                    </span>
                  </div>
                  <div>
                    <strong>Created</strong>
                    <span>{formatTime(artifact.created_at)}</span>
                  </div>
                  <div>
                    <strong>Job</strong>
                    <span>
                      {artifact.job_id ? (
                        <Link to={`/jobs/${artifact.job_id}`}>
                          {jobLabel(artifact)}
                        </Link>
                      ) : (
                        "—"
                      )}
                    </span>
                  </div>
                  <div>
                    <strong>Size</strong>
                    <span>{formatFileSize(artifact.size_bytes)}</span>
                  </div>
                  <div>
                    <strong>Content type</strong>
                    <span>{artifact.content_type ?? "—"}</span>
                  </div>
                  <div className="artifact-version-meta-full">
                    <strong>Digest</strong>
                    <span
                      className="artifact-mono artifact-checksum-value"
                      title={artifact.checksum_sha256 ?? undefined}
                    >
                      {artifact.checksum_sha256
                        ? formatChecksumDisplay(artifact.checksum_sha256)
                        : "—"}
                    </span>
                  </div>
                </div>

                <div className="artifact-build-card-footer">
                  <div className="artifact-actions">
                    <Link to={`/artifacts/${artifact.id}`}>Open artifact</Link>
                    <Link to={`/builds/${artifact.build_id}`}>View build</Link>
                    <a href={artifactDownloadURL(artifact.download_url_path)}>
                      Download
                    </a>
                  </div>
                </div>
              </article>
            ))}
          </div>
        </section>
      )}
    </>
  );
}
