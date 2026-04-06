import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  listBuilds,
  getBuild,
  getBuildSteps,
  getBuildArtifacts,
  artifactDownloadURL,
  listJobs,
  getJob,
  createJob,
  updateJob,
  runJob,
} from "../api/client";

describe("API client - types", () => {
  it("should export API functions", () => {
    expect(typeof listBuilds).toBe("function");
    expect(typeof getBuild).toBe("function");
    expect(typeof getBuildSteps).toBe("function");
    expect(typeof getBuildArtifacts).toBe("function");
    expect(typeof artifactDownloadURL).toBe("function");
    expect(typeof listJobs).toBe("function");
    expect(typeof getJob).toBe("function");
    expect(typeof createJob).toBe("function");
    expect(typeof updateJob).toBe("function");
    expect(typeof runJob).toBe("function");
  });

  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("fetches build artifacts from /builds/{id}/artifacts", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue({
      ok: true,
      json: async () => ({
        data: {
          build_id: "build-1",
          artifacts: [
            {
              id: "artifact-1",
              build_id: "build-1",
              path: "dist/app",
              size_bytes: 128,
              content_type: null,
              checksum_sha256: null,
              download_url_path:
                "/builds/build-1/artifacts/artifact-1/download",
              created_at: "2026-03-24T00:00:01Z",
            },
          ],
        },
      }),
    } as Response);

    const artifacts = await getBuildArtifacts("build-1");
    expect(artifacts).toHaveLength(1);
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/builds/build-1/artifacts",
      undefined,
    );
  });

  it("builds artifact download URL from API base path", () => {
    expect(artifactDownloadURL("/builds/build-1/artifacts/a1/download")).toBe(
      "/api/builds/build-1/artifacts/a1/download",
    );
  });

  it("lists jobs from /jobs", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue({
      ok: true,
      json: async () => ({
        data: {
          jobs: [
            {
              id: "job-1",
              project_id: "project-1",
              name: "backend-ci",
              repository_url: "https://github.com/example/backend.git",
              default_ref: "main",
              push_enabled: true,
              push_branch: "main",
              pipeline_yaml: "version: 1",
              enabled: true,
              created_at: "2026-03-30T00:00:00Z",
              updated_at: "2026-03-30T00:00:00Z",
            },
          ],
        },
      }),
    } as Response);

    const jobs = await listJobs();
    expect(jobs).toHaveLength(1);
    expect(fetchMock).toHaveBeenCalledWith("/api/jobs", undefined);
  });

  it("creates and runs job with expected endpoints", async () => {
    const fetchMock = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          data: {
            id: "job-1",
            project_id: "project-1",
            name: "backend-ci",
            repository_url: "https://github.com/example/backend.git",
            default_ref: "main",
            push_enabled: true,
            push_branch: "main",
            pipeline_yaml: "version: 1",
            enabled: true,
            created_at: "2026-03-30T00:00:00Z",
            updated_at: "2026-03-30T00:00:00Z",
          },
        }),
      } as Response)
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          data: {
            id: "build-1",
            project_id: "project-1",
            status: "queued",
            created_at: "2026-03-30T00:00:00Z",
            queued_at: "2026-03-30T00:00:01Z",
            started_at: null,
            finished_at: null,
            current_step_index: 0,
            error_message: null,
          },
        }),
      } as Response);

    await createJob({
      project_id: "project-1",
      name: "backend-ci",
      repository_url: "https://github.com/example/backend.git",
      default_ref: "main",
      push_enabled: true,
      push_branch: "main",
      pipeline_yaml: "version: 1",
      enabled: true,
    });
    await runJob("job-1");

    expect(fetchMock).toHaveBeenNthCalledWith(1, "/api/jobs", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        project_id: "project-1",
        name: "backend-ci",
        repository_url: "https://github.com/example/backend.git",
        default_ref: "main",
        push_enabled: true,
        push_branch: "main",
        pipeline_yaml: "version: 1",
        enabled: true,
      }),
    });
    expect(fetchMock).toHaveBeenNthCalledWith(2, "/api/jobs/job-1/run", {
      method: "POST",
    });
  });

  it("updates job via PUT /jobs/{id}", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue({
      ok: true,
      json: async () => ({
        data: {
          id: "job-1",
          project_id: "project-1",
          name: "backend-ci",
          repository_url: "https://github.com/example/backend.git",
          default_ref: "main",
          push_enabled: false,
          push_branch: null,
          pipeline_yaml: "version: 1",
          enabled: false,
          created_at: "2026-03-30T00:00:00Z",
          updated_at: "2026-03-30T00:01:00Z",
        },
      }),
    } as Response);

    await updateJob("job-1", { enabled: false });

    expect(fetchMock).toHaveBeenCalledWith("/api/jobs/job-1", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ enabled: false }),
    });
  });
});
