import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { BuildArtifactsSection } from "./BuildArtifactsSection";
import type { BuildArtifact, BuildStep } from "../types";

vi.mock("../api", () => ({
  artifactDownloadURL: (path: string) => `/api${path}`,
}));

function makeArtifact(overrides: Partial<BuildArtifact> = {}): BuildArtifact {
  return {
    id: "artifact-1",
    build_id: "build-1",
    step_id: null,
    name: "artifact-1",
    path: "dist/artifact-1",
    size_bytes: 1024,
    content_type: "application/octet-stream",
    checksum_sha256: null,
    storage_provider: "filesystem",
    download_url_path: "/builds/build-1/artifacts/artifact-1/download",
    version_tags: [],
    created_at: "2026-04-01T00:00:00Z",
    ...overrides,
  };
}

function makeStep(overrides: Partial<BuildStep> = {}): BuildStep {
  return {
    id: "step-1",
    build_id: "build-1",
    step_index: 1,
    name: "package",
    command: "make package",
    status: "success",
    worker_id: null,
    started_at: "2026-04-01T00:00:00Z",
    finished_at: "2026-04-01T00:01:00Z",
    exit_code: 0,
    stdout: null,
    stderr: null,
    error_message: null,
    ...overrides,
  };
}

function renderSection(
  props: Partial<React.ComponentProps<typeof BuildArtifactsSection>> = {},
) {
  return render(
    <MemoryRouter>
      <BuildArtifactsSection
        artifacts={[]}
        isLoading={false}
        error={null}
        {...props}
      />
    </MemoryRouter>,
  );
}

describe("BuildArtifactsSection", () => {
  it("shows loading, error, and empty states", () => {
    const { rerender } = render(
      <MemoryRouter>
        <BuildArtifactsSection artifacts={[]} isLoading error={null} />
      </MemoryRouter>,
    );

    expect(screen.getByText("Loading artifacts…")).toBeTruthy();

    rerender(
      <MemoryRouter>
        <BuildArtifactsSection
          artifacts={[]}
          isLoading={false}
          error={new Error("nope")}
        />
      </MemoryRouter>,
    );
    expect(
      screen.getByText("Failed to load artifacts: Error: nope"),
    ).toBeTruthy();

    rerender(
      <MemoryRouter>
        <BuildArtifactsSection artifacts={[]} isLoading={false} error={null} />
      </MemoryRouter>,
    );
    expect(
      screen.getByText("No artifacts were collected for this build."),
    ).toBeTruthy();
  });

  it("renders grouped artifacts, checksum previews, and download actions", () => {
    const longChecksum =
      "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef";

    renderSection({
      artifacts: [
        makeArtifact({
          id: "artifact-build",
          name: "  dist/app  ",
          path: "dist/app",
          checksum_sha256: longChecksum,
          created_at: "2026-04-01T00:02:00Z",
          download_url_path:
            "/builds/build-1/artifacts/artifact-build/download",
        }),
        makeArtifact({
          id: "artifact-step-known",
          name: "package.tar.gz",
          path: "out/package.tar.gz",
          step_id: "step-2",
          created_at: "2026-04-01T00:01:00Z",
        }),
        makeArtifact({
          id: "artifact-step-unknown",
          name: undefined,
          path: "out/manifest.json",
          step_id: "step-unknown-1",
          created_at: "2026-04-01T00:00:30Z",
        }),
      ],
      steps: [makeStep({ id: "step-2", step_index: 2, name: "package" })],
    });

    expect(screen.getByText("Build-level")).toBeTruthy();
    expect(screen.getByText("Step 2: package")).toBeTruthy();
    expect(screen.getByText("Step step-unk…")).toBeTruthy();

    const checksum = screen.getByText("0123456789ab…89abcdef");
    expect(checksum).toHaveAttribute("title", longChecksum);

    expect(screen.getByRole("link", { name: "dist/app" })).toHaveAttribute(
      "href",
      "/artifacts/artifact-build",
    );
    expect(
      screen
        .getAllByRole("link", { name: "Download" })
        .some(
          (link) =>
            link.getAttribute("href") ===
            "/api/builds/build-1/artifacts/artifact-build/download",
        ),
    ).toBe(true);

    const subtlePathLines = screen.queryAllByText("dist/app", {
      selector: "div.subtle-text.artifact-mono",
    });
    expect(subtlePathLines).toHaveLength(0);
  });

  it("submits version assignments when enabled and hides the form otherwise", async () => {
    const onAssignVersion = vi.fn().mockResolvedValue(undefined);

    const { rerender } = render(
      <MemoryRouter>
        <BuildArtifactsSection
          artifacts={[makeArtifact({ id: "artifact-assign" })]}
          isLoading={false}
          error={null}
          onAssignVersion={onAssignVersion}
        />
      </MemoryRouter>,
    );

    const input = screen.getByLabelText("artifact-version-artifact-assign");
    fireEvent.change(input, { target: { value: "1.2.3" } });
    fireEvent.submit(input.closest("form") as HTMLFormElement);

    await waitFor(() => {
      expect(onAssignVersion).toHaveBeenCalledWith("artifact-assign", "1.2.3");
    });

    rerender(
      <MemoryRouter>
        <BuildArtifactsSection
          artifacts={[makeArtifact({ id: "artifact-assign" })]}
          isLoading={false}
          error={null}
        />
      </MemoryRouter>,
    );

    expect(
      screen.queryByLabelText("artifact-version-artifact-assign"),
    ).toBeNull();
  });
});
