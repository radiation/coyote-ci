import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  APIError,
  formatAPIErrorMessage,
  listBuilds,
  getBuild,
  getBuildSteps,
  getBuildArtifacts,
  listArtifacts,
  listProjects,
  getProject,
  createProject,
  listJobsByProject,
  createJobVersionTags,
  artifactDownloadURL,
  listJobs,
  getJob,
  createJob,
  updateJob,
  runJob,
  listSourceCredentials,
  getMe,
  listUsers,
  createUser,
  updateUser,
  deleteUser,
  listProjectMembers,
  upsertProjectMember,
  updateProjectMember,
  deleteProjectMember,
  isAPIErrorStatus,
} from "../api/client";

describe("API client - types", () => {
  it("should export API functions", () => {
    expect(typeof listBuilds).toBe("function");
    expect(typeof getBuild).toBe("function");
    expect(typeof getBuildSteps).toBe("function");
    expect(typeof getBuildArtifacts).toBe("function");
    expect(typeof listProjects).toBe("function");
    expect(typeof getProject).toBe("function");
    expect(typeof createProject).toBe("function");
    expect(typeof listJobsByProject).toBe("function");
    expect(typeof createJobVersionTags).toBe("function");
    expect(typeof artifactDownloadURL).toBe("function");
    expect(typeof listJobs).toBe("function");
    expect(typeof getJob).toBe("function");
    expect(typeof createJob).toBe("function");
    expect(typeof updateJob).toBe("function");
    expect(typeof runJob).toBe("function");
    expect(typeof listSourceCredentials).toBe("function");
    expect(typeof getMe).toBe("function");
    expect(typeof listUsers).toBe("function");
    expect(typeof createUser).toBe("function");
    expect(typeof updateUser).toBe("function");
    expect(typeof deleteUser).toBe("function");
    expect(typeof listProjectMembers).toBe("function");
    expect(typeof upsertProjectMember).toBe("function");
    expect(typeof updateProjectMember).toBe("function");
    expect(typeof deleteProjectMember).toBe("function");
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

  it("lists artifacts with search, type, and pagination params", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue({
      ok: true,
      json: async () => ({
        data: {
          artifacts: [],
        },
      }),
    } as Response);

    await listArtifacts({
      q: "pkg",
      type: "npm_package",
      limit: 5,
      offset: 10,
    });

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/artifacts?q=pkg&type=npm_package&limit=5&offset=10",
      undefined,
    );
  });

  it("creates immutable job version tags", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue({
      ok: true,
      json: async () => ({
        data: {
          job_id: "job-1",
          version: "v1",
          tags: [
            {
              id: "tag-1",
              job_id: "job-1",
              version: "v1",
              target_type: "artifact",
              artifact_id: "artifact-1",
              created_at: "2026-04-22T00:00:00Z",
            },
          ],
        },
      }),
    } as Response);

    const tags = await createJobVersionTags("job-1", {
      version: "v1",
      artifact_ids: ["artifact-1"],
    });

    expect(tags).toHaveLength(1);
    expect(fetchMock).toHaveBeenCalledWith("/api/jobs/job-1/version-tags", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ version: "v1", artifact_ids: ["artifact-1"] }),
    });
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
              managed_image: null,
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

  it("lists and creates projects from /projects", async () => {
    const fetchMock = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          data: {
            projects: [
              {
                id: "project-1",
                name: "Platform",
                slug: "platform",
                description: "Core platform pipelines",
                created_at: "2026-05-01T00:00:00Z",
                updated_at: "2026-05-01T00:00:00Z",
              },
            ],
          },
        }),
      } as Response)
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          data: {
            id: "project-2",
            name: "Release",
            slug: "release",
            description: "Release automation",
            created_at: "2026-05-01T00:00:00Z",
            updated_at: "2026-05-01T00:00:00Z",
          },
        }),
      } as Response);

    const projects = await listProjects();
    await createProject({
      name: "Release",
      slug: "release",
      description: "Release automation",
    });

    expect(projects).toHaveLength(1);
    expect(fetchMock).toHaveBeenNthCalledWith(1, "/api/projects", undefined);
    expect(fetchMock).toHaveBeenNthCalledWith(2, "/api/projects", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        name: "Release",
        slug: "release",
        description: "Release automation",
      }),
    });
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
            managed_image: null,
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
      managed_image: {
        enabled: true,
        managed_image_name: "go",
        pipeline_path: ".coyote/pipeline.yml",
        write_credential_id: "cred-1",
      },
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
        managed_image: {
          enabled: true,
          managed_image_name: "go",
          pipeline_path: ".coyote/pipeline.yml",
          write_credential_id: "cred-1",
        },
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
          managed_image: null,
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

  it("lists source credentials from /source-credentials", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue({
      ok: true,
      json: async () => ({
        data: {
          credentials: [
            {
              id: "cred-1",
              name: "github-bot",
              kind: "https_token",
              username: "x-access-token",
              secret_ref: "COYOTE_TOKEN",
              created_at: "2026-03-30T00:00:00Z",
              updated_at: "2026-03-30T00:00:00Z",
            },
          ],
        },
      }),
    } as Response);

    const credentials = await listSourceCredentials();
    expect(credentials).toHaveLength(1);
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/source-credentials",
      undefined,
    );
  });

  it("fetches disabled-mode current user from /me", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue({
      ok: true,
      json: async () => ({
        data: {
          auth_mode: "disabled",
          user: {
            id: "disabled-mode-user",
            email: "dev@local.coyote-ci",
            global_role: "admin",
          },
        },
      }),
    } as Response);

    const me = await getMe();
    expect(me.auth_mode).toBe("disabled");
    expect(fetchMock).toHaveBeenCalledWith("/api/me", undefined);
  });

  it("uses identity and project membership endpoints", async () => {
    const fetchMock = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({ data: { users: [] } }),
      } as Response)
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          data: { id: "user-1", email: "dev@example.com", global_role: "user" },
        }),
      } as Response)
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          data: {
            id: "user-1",
            email: "dev@example.com",
            global_role: "admin",
          },
        }),
      } as Response)
      .mockResolvedValueOnce({ ok: true, text: async () => "" } as Response)
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({ data: { members: [] } }),
      } as Response)
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          data: { project_id: "project-1", user_id: "user-1", role: "viewer" },
        }),
      } as Response)
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          data: { project_id: "project-1", user_id: "user-1", role: "owner" },
        }),
      } as Response)
      .mockResolvedValueOnce({ ok: true, text: async () => "" } as Response);

    await listUsers();
    await createUser({ email: "dev@example.com", global_role: "user" });
    await updateUser("user-1", { global_role: "admin" });
    await deleteUser("user-1");
    await listProjectMembers("project-1");
    await upsertProjectMember("project-1", "user-1", "viewer");
    await updateProjectMember("project-1", "user-1", "owner");
    await deleteProjectMember("project-1", "user-1");

    expect(fetchMock).toHaveBeenNthCalledWith(1, "/api/users", undefined);
    expect(fetchMock).toHaveBeenNthCalledWith(2, "/api/users", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ email: "dev@example.com", global_role: "user" }),
    });
    expect(fetchMock).toHaveBeenNthCalledWith(3, "/api/users/user-1", {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ global_role: "admin" }),
    });
    expect(fetchMock).toHaveBeenNthCalledWith(4, "/api/users/user-1", {
      method: "DELETE",
    });
    expect(fetchMock).toHaveBeenNthCalledWith(
      5,
      "/api/projects/project-1/members",
      undefined,
    );
    expect(fetchMock).toHaveBeenNthCalledWith(
      6,
      "/api/projects/project-1/members/user-1",
      {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ role: "viewer" }),
      },
    );
    expect(fetchMock).toHaveBeenNthCalledWith(
      7,
      "/api/projects/project-1/members/user-1",
      {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ role: "owner" }),
      },
    );
    expect(fetchMock).toHaveBeenNthCalledWith(
      8,
      "/api/projects/project-1/members/user-1",
      { method: "DELETE" },
    );
  });

  it("formats API auth and forbidden errors for small UI surfaces", () => {
    const unauthorized = new APIError(401, "missing user email header");
    const forbidden = new APIError(403, "global admin is required");
    const generic = new APIError(500, "internal server error");

    expect(isAPIErrorStatus(unauthorized, 401)).toBe(true);
    expect(isAPIErrorStatus(unauthorized, 403)).toBe(false);
    expect(isAPIErrorStatus(new Error("boom"), 401)).toBe(false);

    expect(
      formatAPIErrorMessage(unauthorized, "fallback forbidden message"),
    ).toContain("configured for external authentication");
    expect(formatAPIErrorMessage(forbidden, "fallback forbidden message")).toBe(
      "fallback forbidden message",
    );
    expect(
      formatAPIErrorMessage(
        generic,
        "fallback forbidden message",
        "Failed to load users",
      ),
    ).toBe("Failed to load users: API 500: internal server error");
    expect(
      formatAPIErrorMessage("raw failure", "fallback forbidden message"),
    ).toBe("raw failure");
  });

  it("throws APIError for JSON and plain-text error responses", async () => {
    vi.spyOn(globalThis, "fetch")
      .mockResolvedValueOnce({
        ok: false,
        status: 403,
        text: async () =>
          JSON.stringify({ error: { message: "global admin is required" } }),
      } as Response)
      .mockResolvedValueOnce({
        ok: false,
        status: 500,
        text: async () => "internal server error",
      } as Response);

    await expect(listUsers()).rejects.toMatchObject({
      name: "APIError",
      status: 403,
      message: "API 403: global admin is required",
    });
    await expect(deleteUser("user-1")).rejects.toMatchObject({
      name: "APIError",
      status: 500,
      message: "API 500: internal server error",
    });
  });
});
