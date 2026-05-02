import {
  useDeferredValue,
  useEffect,
  useRef,
  useState,
  type FormEvent,
} from "react";
import {
  keepPreviousData,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { createJobVersionTags, listArtifacts } from "../api";
import { ArtifactBrowser } from "../components/ArtifactBrowser";
import type { ArtifactBrowseVersion, ArtifactType } from "../types";
import { formatTime } from "../utils/time";
import { useLocation, useSearchParams } from "react-router-dom";

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

const DEFAULT_ARTIFACTS_PAGE_SIZE = 10;
const ARTIFACTS_PAGE_SIZE_OPTIONS = [10, 25, 50];
const COPY_STATUS_RESET_MS = 2000;

function parseArtifactTypeParam(value: string | null): ArtifactType | "" {
  if (
    value === "docker_image" ||
    value === "npm_package" ||
    value === "generic" ||
    value === "unknown"
  ) {
    return value;
  }
  return "";
}

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

export function ArtifactsPage() {
  const location = useLocation();
  const [searchParams, setSearchParams] = useSearchParams();
  const queryClient = useQueryClient();
  const search = searchParams.get("q") ?? "";
  const typeFilter = parseArtifactTypeParam(searchParams.get("type"));
  const pageIndex = parsePositiveInt(searchParams.get("page"), 1) - 1;
  const pageSize = parsePageSizeParam(searchParams.get("pageSize"));
  const [copyStatus, setCopyStatus] = useState<"idle" | "copied" | "failed">(
    "idle",
  );
  const pageInputRef = useRef<HTMLInputElement | null>(null);
  const trimmedSearch = search.trim();
  const deferredSearch = useDeferredValue(trimmedSearch);
  const hasActiveFilters = Boolean(trimmedSearch || typeFilter);
  const activeFilterCount = (trimmedSearch ? 1 : 0) + (typeFilter ? 1 : 0);

  useEffect(() => {
    if (copyStatus === "idle") {
      return undefined;
    }

    const timeoutID = globalThis.setTimeout(() => {
      setCopyStatus("idle");
    }, COPY_STATUS_RESET_MS);

    return () => globalThis.clearTimeout(timeoutID);
  }, [copyStatus]);

  useEffect(() => {
    if (pageInputRef.current) {
      pageInputRef.current.value = String(pageIndex + 1);
    }
  }, [pageIndex]);

  function updateSearchParams(
    mutate: (nextParams: URLSearchParams) => void,
  ) {
    const draftParams = new URLSearchParams(searchParams);
    mutate(draftParams);

    const nextParams = new URLSearchParams();
    for (const key of ["q", "type", "page", "pageSize"]) {
      const value = draftParams.get(key);
      if (value) {
        nextParams.set(key, value);
      }
    }

    if (nextParams.toString() !== searchParams.toString()) {
      setSearchParams(nextParams, { replace: true });
    }
  }

  const {
    data: artifactResults,
    isLoading,
    isFetching,
    error,
    dataUpdatedAt,
  } = useQuery({
    queryKey: ["artifacts", deferredSearch, typeFilter, pageIndex, pageSize],
    queryFn: () =>
      listArtifacts({
        q: deferredSearch,
        type: typeFilter,
        limit: pageSize + 1,
        offset: pageIndex * pageSize,
      }),
    placeholderData: keepPreviousData,
  });

  const artifacts = artifactResults?.slice(0, pageSize) ?? [];
  const hasNextPage = (artifactResults?.length ?? 0) > pageSize;
  const pageStart = pageIndex * pageSize + 1;
  const pageEnd = pageIndex * pageSize + artifacts.length;

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

  function handleTypeChange(value: ArtifactType | "") {
    updateSearchParams((nextParams) => {
      if (value) {
        nextParams.set("type", value);
      } else {
        nextParams.delete("type");
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

  function handlePageJumpSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const formData = new FormData(event.currentTarget);
    const pageValue = formData.get("page");
    const parsedPage =
      typeof pageValue === "string" ? Number.parseInt(pageValue, 10) : NaN;
    if (!Number.isFinite(parsedPage) || parsedPage < 1) {
      if (pageInputRef.current) {
        pageInputRef.current.value = String(pageIndex + 1);
      }
      return;
    }
    goToPage(parsedPage);
  }

  async function handleCopyLink() {
    const baseURL = globalThis.location?.origin ?? "";
    const shareURL = `${baseURL}${location.pathname}${location.search}${location.hash}`;

    try {
      if (!globalThis.navigator?.clipboard?.writeText) {
        throw new Error("Clipboard API unavailable");
      }
      await globalThis.navigator.clipboard.writeText(shareURL);
      setCopyStatus("copied");
    } catch {
      setCopyStatus("failed");
    }
  }

  return (
    <>
      <div className="page-header-row">
        <div>
          <h2>Artifacts</h2>
          <p className="subtle-text">
            Latest repository refresh:{" "}
            {dataUpdatedAt > 0
              ? formatTime(new Date(dataUpdatedAt).toISOString())
              : "—"}
          </p>
        </div>
        <div className="page-header-actions">
          <button
            type="button"
            className="secondary-button"
            onClick={handleCopyLink}
          >
            Copy link
          </button>
          {copyStatus === "copied" && (
            <p className="subtle-text artifact-copy-status">Link copied.</p>
          )}
          {copyStatus === "failed" && (
            <p className="error-text artifact-copy-status">
              Unable to copy link.
            </p>
          )}
        </div>
      </div>

      <section className="artifact-filters-panel" aria-label="Artifact filters">
        <div className="artifact-filter-toolbar">
          <div>
            <h3>Repository Browse</h3>
            <p className="subtle-text">
              {activeFilterCount > 0
                ? `${activeFilterCount} active filter${activeFilterCount === 1 ? "" : "s"}`
                : "All logical artifacts"}
              {isFetching && !isLoading ? " · Updating" : ""}
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
            placeholder="Name, path, project, job, or version"
          />
        </label>
        <label className="artifact-filter-field artifact-filter-select">
          <span>Type</span>
          <select
            value={typeFilter}
            onChange={(event) =>
              handleTypeChange(event.target.value as ArtifactType | "")
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
          <form
            className="artifact-pagination-jump-form"
            onSubmit={handlePageJumpSubmit}
          >
            <label className="artifact-pagination-field">
              <span>Jump to page</span>
              <input
                ref={pageInputRef}
                name="page"
                type="number"
                min={1}
                inputMode="numeric"
                defaultValue={String(pageIndex + 1)}
                disabled={isFetching}
              />
            </label>
            <button
              type="submit"
              className="secondary-button"
              disabled={isFetching}
            >
              Go
            </button>
          </form>
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
