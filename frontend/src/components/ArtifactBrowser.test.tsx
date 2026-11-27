import { describe, it, expect, vi } from "vitest";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { ArtifactBrowser } from "./ArtifactBrowser";
import type { ArtifactBrowseItem } from "../types";

vi.mock("../api", () => ({
  artifactDownloadURL: (path: string) => `/api${path}`,
}));

function makeArtifact(
  overrides: Partial<ArtifactBrowseItem> = {},
): ArtifactBrowseItem {
  return {
    key: "project-1:packages/pkg-a.tgz",
    name: "coyote-ci/package-a",
    path: "packages/pkg-a.tgz",
    project_id: "project-1",
    project_name: "Platform",
    project_slug: "platform",
    job_id: "job-1",
    job_name: "backend-ci",
    artifact_type: "npm_package",
    latest_created_at: "2026-04-25T09:00:00Z",
    versions: [
      {
        artifact_id: "artifact-1",
        name: "coyote-ci/package-a",
        build_id: "build-1",
        build_number: 41,
        build_status: "success",
        project_id: "project-1",
        project_name: "Platform",
        project_slug: "platform",
        job_id: "job-1",
        job_name: "backend-ci",
        step_id: "step-1",
        step_index: 1,
        step_name: "Publish package",
        path: "packages/pkg-a.tgz",
        size_bytes: 1024,
        content_type: "application/gzip",
        checksum_sha256: "sha-1",
        storage_provider: "filesystem",
        download_url_path: "/builds/build-1/artifacts/artifact-1/download",
        lineage: {
          project_id: "project-1",
          project_name: "Platform",
          job_id: "job-1",
          job_name: "backend-ci",
          build_id: "build-1",
          build_number: 41,
          artifact_id: "artifact-1",
          artifact_name: "coyote-ci/package-a",
          artifact_path: "packages/pkg-a.tgz",
          versions: ["2026.04.25"],
          channels: ["latest"],
          git_ref: "refs/heads/main",
          git_sha: "95f09eb123456789",
          created_at: "2026-04-25T09:00:00Z",
        },
        version_tags: [
          {
            id: "tag-version-1",
            job_id: "job-1",
            kind: "version",
            version: "2026.04.25",
            target_type: "artifact",
            artifact_id: "artifact-1",
            created_at: "2026-04-25T09:00:00Z",
          },
          {
            id: "tag-channel-1",
            job_id: "job-1",
            kind: "channel",
            version: "latest",
            target_type: "artifact",
            artifact_id: "artifact-1",
            created_at: "2026-04-25T09:00:00Z",
          },
        ],
        created_at: "2026-04-25T09:00:00Z",
      },
    ],
    ...overrides,
  };
}

function renderBrowser(
  props: Partial<React.ComponentProps<typeof ArtifactBrowser>> = {},
) {
  return render(
    <MemoryRouter>
      <Routes>
        <Route
          path="*"
          element={
            <ArtifactBrowser
              artifacts={[]}
              isLoading={false}
              error={null}
              {...props}
            />
          }
        />
      </Routes>
    </MemoryRouter>,
  );
}

describe("ArtifactBrowser", () => {
  it("keeps same-path packages separate and resolves multiple labels to an older matching output", () => {
    const pkg = makeArtifact();
    const older = pkg.versions[0];
    older.version_tags!.push({
      ...older.version_tags![0],
      id: "second",
      version: "1.2.0",
    });
    const newest = {
      ...older,
      artifact_id: "new",
      build_id: "new-build",
      build_number: 42,
      version_tags: [],
      lineage: null,
    };
    renderBrowser({
      artifacts: [
        { ...pkg, versions: [newest, older] },
        { ...pkg, key: "other", job_id: null, versions: [newest] },
      ],
    });
    const toggle = screen.getByRole("button", { name: /Job backend-ci/ });
    expect(toggle).toHaveTextContent("Newest matching output: Build #42");
    fireEvent.click(toggle);
    expect(toggle).toHaveAttribute("aria-expanded", "true");
    expect(
      screen.getByRole("button", { name: /Jobless build new-build/ }),
    ).toHaveAttribute("aria-expanded", "false");
    const channels = within(
      screen.getByText("Mutable aliases").closest("section")!,
    );
    expect(channels.getByText("latest")).toBeVisible();
    expect(channels.getByRole("link", { name: "Download" })).toHaveAttribute(
      "href",
      `/api${older.download_url_path}`,
    );
    const versions = within(
      screen
        .getByRole("heading", { name: "Immutable versions" })
        .closest("section")!,
    );
    expect(versions.getAllByText("1.2.0")[0]).toBeVisible();
    expect(versions.getAllByText("2026.04.25")[0]).toBeVisible();
    const unversioned = within(
      screen
        .getByRole("heading", { name: "Unversioned outputs" })
        .closest("section")!,
    );
    expect(
      unversioned.getByRole("link", { name: "Open artifact" }),
    ).toHaveAttribute("href", "/artifacts/new");
  });
  it("renders loading, error, and empty states", () => {
    const { rerender } = render(
      <MemoryRouter>
        <ArtifactBrowser artifacts={[]} isLoading error={null} />
      </MemoryRouter>,
    );

    expect(screen.getByLabelText("Loading artifacts")).toBeTruthy();

    rerender(
      <MemoryRouter>
        <ArtifactBrowser
          artifacts={[]}
          isLoading={false}
          error={new Error("boom")}
        />
      </MemoryRouter>,
    );
    expect(
      screen.getByText("Failed to load artifacts: Error: boom"),
    ).toBeTruthy();

    rerender(
      <MemoryRouter>
        <ArtifactBrowser
          artifacts={[]}
          isLoading={false}
          error={null}
          hasActiveFilters
        />
      </MemoryRouter>,
    );
    expect(
      screen.getByText("No packages matched the current filters."),
    ).toBeTruthy();

    rerender(
      <MemoryRouter>
        <ArtifactBrowser
          artifacts={[]}
          isLoading={false}
          error={null}
          pageIndex={1}
        />
      </MemoryRouter>,
    );
    expect(screen.getByText("No artifacts on this page.")).toBeTruthy();
  });

  it("renders expanded channel and version fallbacks for sparse artifacts", () => {
    renderBrowser({
      artifacts: [
        makeArtifact({
          name: undefined,
          job_id: undefined,
          job_name: undefined,
          versions: [
            {
              artifact_id: "artifact-1",
              build_id: "build-1",
              build_number: 0,
              build_status: "failed",
              project_id: "project-1",
              project_name: undefined,
              project_slug: undefined,
              job_id: undefined,
              job_name: undefined,
              step_id: null,
              step_index: null,
              step_name: null,
              path: "packages/pkg-a.tgz",
              size_bytes: 1024,
              content_type: null,
              checksum_sha256: null,
              storage_provider: "filesystem",
              download_url_path:
                "/builds/build-1/artifacts/artifact-1/download",
              version_tags: [],
              created_at: "2026-04-25T09:00:00Z",
            },
          ],
        }),
      ],
    });

    fireEvent.click(
      screen.getByRole("button", { name: /packages\/pkg-a\.tgz/i }),
    );

    expect(screen.getByText("No channels on matching outputs.")).toBeTruthy();
    expect(screen.getByText("No versions yet")).toBeTruthy();
    expect(screen.getByText("No channels yet")).toBeTruthy();
    expect(screen.getAllByText("Build-level artifact").length).toBeGreaterThan(
      0,
    );
    expect(screen.getAllByText("—").length).toBeGreaterThan(0);
    expect(screen.getByRole("link", { name: "Open artifact" })).toHaveAttribute(
      "href",
      "/artifacts/artifact-1",
    );
  });

  it("toggles artifacts with no versions and channel-only resolution labels", () => {
    renderBrowser({
      artifacts: [
        makeArtifact({
          key: "artifact-empty",
          path: "dist/empty.txt",
          versions: [],
        }),
        makeArtifact({
          key: "artifact-channel-only",
          path: "dist/channel-only.txt",
          versions: [
            {
              artifact_id: "artifact-2",
              build_id: "build-2",
              build_number: 0,
              build_status: "success",
              project_id: "project-1",
              project_name: "Platform",
              project_slug: "platform",
              job_id: "job-1",
              job_name: "backend-ci",
              step_id: "step-2",
              step_index: 2,
              step_name: "Publish package",
              path: "dist/channel-only.txt",
              size_bytes: 2048,
              content_type: "text/plain",
              checksum_sha256: "sha-2",
              storage_provider: "filesystem",
              download_url_path:
                "/builds/build-2/artifacts/artifact-2/download",
              version_tags: [
                {
                  id: "tag-channel-2",
                  job_id: "job-1",
                  kind: "channel",
                  version: "stable",
                  target_type: "artifact",
                  artifact_id: "artifact-2",
                  created_at: "2026-04-25T09:00:00Z",
                },
              ],
              created_at: "2026-04-25T09:00:00Z",
            },
          ],
        }),
      ],
    });

    expect(screen.getByText("No matching outputs")).toBeTruthy();

    fireEvent.click(
      screen.getByRole("button", { name: /dist\/channel-only\.txt/i }),
    );

    expect(screen.getByText("Points to Build build-2…")).toBeTruthy();
    expect(screen.getAllByText("1 channel").length).toBeGreaterThan(0);
  });

  it("renders compact lineage details for expanded versions", () => {
    renderBrowser({
      artifacts: [makeArtifact()],
    });

    fireEvent.click(
      screen.getByRole("button", { name: /coyote-ci\/package-a/i }),
    );

    expect(screen.getAllByText("Commit 95f09eb").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Version 2026.04.25").length).toBeGreaterThan(0);
    expect(screen.getByText("Channel latest")).toBeTruthy();
    expect(
      screen.getAllByRole("link", { name: "Build #41" }).length,
    ).toBeGreaterThan(0);
  });
});
