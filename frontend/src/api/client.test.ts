import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  APIError,
  formatAPIErrorMessage,
  getArtifact,
  listBuilds,
  getBuild,
  cancelBuild,
  rerunBuild,
  listQueue,
  getBuildSteps,
  getStepLogs,
  getBuildArtifacts,
  listArtifactCatalog,
  listArtifacts,
  listProjects,
  getProject,
  createProject,
  listJobsByProject,
  createJobVersionTags,
  artifactDownloadURL,
  buildStepLogStreamURL,
  listJobs,
  getJob,
  createJob,
  updateJob,
  runJob,
  listSourceCredentials,
  getAuthConfig,
  getMe,
  authLoginURL,
  logoutSession,
  listUsers,
  createUser,
  updateUser,
  deleteUser,
  listAPITokens,
  createAPIToken,
  revokeAPIToken,
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
    expect(typeof cancelBuild).toBe("function");
    expect(typeof getBuildSteps).toBe("function");
    expect(typeof getBuildArtifacts).toBe("function");
    expect(typeof listArtifactCatalog).toBe("function");
    expect(typeof getArtifact).toBe("function");
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
    expect(typeof getAuthConfig).toBe("function");
    expect(typeof getMe).toBe("function");
    expect(typeof authLoginURL).toBe("function");
    expect(typeof logoutSession).toBe("function");
    expect(typeof listUsers).toBe("function");
    expect(typeof createUser).toBe("function");
    expect(typeof updateUser).toBe("function");
    expect(typeof deleteUser).toBe("function");
    expect(typeof listAPITokens).toBe("function");
    expect(typeof createAPIToken).toBe("function");
    expect(typeof revokeAPIToken).toBe("function");
    expect(typeof listProjectMembers).toBe("function");
    expect(typeof upsertProjectMember).toBe("function");
    expect(typeof updateProjectMember).toBe("function");
    expect(typeof deleteProjectMember).toBe("function");
  });

  it("cancels a build via /builds/{id}/cancel", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue({
      ok: true,
      json: async () => ({
        data: {
          id: "build-1",
          project_id: "project-1",
          priority: 5,
          status: "canceled",
          created_at: "2026-03-24T00:00:00Z",
          queued_at: "2026-03-24T00:00:01Z",
          started_at: null,
          finished_at: "2026-03-24T00:00:30Z",
          current_step_index: 0,
          error_message: "build canceled by operator request",
        },
      }),
    } as Response);

    const build = await cancelBuild("build-1");

    expect(build.status).toBe("canceled");
    expect(fetchMock).toHaveBeenCalledWith("/api/builds/build-1/cancel", {
      credentials: "include",
      method: "POST",
    });
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
              artifact_type: "generic",
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
    expect(artifacts[0]?.artifact_type).toBe("generic");
    expect(fetchMock).toHaveBeenCalledWith("/api/builds/build-1/artifacts", {
      credentials: "include",
    });
  });

  it("lists builds with trimmed project and pagination filters", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue({
      ok: true,
      json: async () => ({
        data: {
          builds: [
            {
              id: "build-1",
              project_id: "project-1",
              priority: 0,
              status: "success",
              created_at: "2026-03-24T00:00:00Z",
              queued_at: "2026-03-24T00:00:01Z",
              started_at: "2026-03-24T00:00:02Z",
              finished_at: "2026-03-24T00:01:00Z",
              current_step_index: 1,
              attempt_number: 1,
              error_message: null,
            },
          ],
        },
      }),
    } as Response);

    const builds = await listBuilds({
      project_id: " project-1 ",
      project_slug: " platform ",
      limit: 50,
      offset: 10,
    });

    expect(builds).toHaveLength(1);
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/builds?project_id=project-1&project_slug=platform&limit=50&offset=10",
      { credentials: "include" },
    );
  });

  it("lists builds without query parameters when filters are empty", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue({
      ok: true,
      json: async () => ({ data: { builds: [] } }),
    } as Response);

    const builds = await listBuilds({
      project_id: " ",
      project_slug: " ",
      limit: 0,
      offset: -1,
    });

    expect(builds).toEqual([]);
    expect(fetchMock).toHaveBeenCalledWith("/api/builds", {
      credentials: "include",
    });
  });

  it("fetches build detail, steps, logs, queue entries, and reruns builds", async () => {
    const fetchMock = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          data: {
            id: "build-1",
            project_id: "project-1",
            priority: 5,
            status: "running",
            created_at: "2026-03-24T00:00:00Z",
            queued_at: "2026-03-24T00:00:01Z",
            started_at: "2026-03-24T00:00:02Z",
            finished_at: null,
            current_step_index: 1,
            error_message: null,
          },
        }),
      } as Response)
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          data: {
            build_id: "build-1",
            steps: [
              {
                id: "step-1",
                build_id: "build-1",
                step_index: 0,
                name: "test",
                command: "go",
                args: ["test", "./..."],
                env: {},
                working_dir: ".",
                timeout_seconds: 0,
                status: "success",
                started_at: null,
                finished_at: null,
                exit_code: 0,
                error_message: null,
              },
            ],
          },
        }),
      } as Response)
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          data: {
            build_id: "build-1",
            step_index: 0,
            after: 2,
            next_sequence: 3,
            chunks: [],
          },
        }),
      } as Response)
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          data: {
            entries: [
              {
                build: {
                  id: "build-1",
                  project_id: "project-1",
                  priority: 5,
                  status: "queued",
                  created_at: "2026-03-24T00:00:00Z",
                  queued_at: "2026-03-24T00:00:01Z",
                  started_at: null,
                  finished_at: null,
                  current_step_index: 0,
                  error_message: null,
                },
                queued_at: "2026-03-24T00:00:01Z",
              },
            ],
          },
        }),
      } as Response)
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          data: {
            id: "build-2",
            project_id: "project-1",
            priority: 5,
            status: "queued",
            created_at: "2026-03-24T00:01:00Z",
            queued_at: "2026-03-24T00:01:01Z",
            started_at: null,
            finished_at: null,
            current_step_index: 0,
            error_message: null,
          },
        }),
      } as Response);

    const build = await getBuild("build-1");
    const steps = await getBuildSteps("build-1");
    const logs = await getStepLogs("build-1", 0, 2, 25);
    const queue = await listQueue({
      project_id: " project-1 ",
      project_slug: " platform ",
      status: "queued",
    });
    const rerun = await rerunBuild("build-1");

    expect(build.status).toBe("running");
    expect(steps).toHaveLength(1);
    expect(logs.next_sequence).toBe(3);
    expect(queue).toHaveLength(1);
    expect(rerun.id).toBe("build-2");
    expect(fetchMock).toHaveBeenNthCalledWith(1, "/api/builds/build-1", {
      credentials: "include",
    });
    expect(fetchMock).toHaveBeenNthCalledWith(2, "/api/builds/build-1/steps", {
      credentials: "include",
    });
    expect(fetchMock).toHaveBeenNthCalledWith(
      3,
      "/api/builds/build-1/steps/0/logs?after=2&limit=25",
      { credentials: "include" },
    );
    expect(fetchMock).toHaveBeenNthCalledWith(
      4,
      "/api/queue?project_id=project-1&project_slug=platform&status=queued",
      { credentials: "include" },
    );
    expect(fetchMock).toHaveBeenNthCalledWith(5, "/api/builds/build-1/rerun", {
      credentials: "include",
      method: "POST",
    });
  });

  it("builds artifact download URL from API base path", () => {
    expect(artifactDownloadURL("/builds/build-1/artifacts/a1/download")).toBe(
      "/api/builds/build-1/artifacts/a1/download",
    );
  });

  it("builds artifact download URL from a relative path without duplicating /api", () => {
    expect(artifactDownloadURL("builds/build-1/artifacts/a1/download")).toBe(
      "/api/builds/build-1/artifacts/a1/download",
    );
  });

  it("builds step log stream URLs with encoded build IDs", () => {
    expect(buildStepLogStreamURL("build/one", 3, 12)).toBe(
      "/api/builds/build%2Fone/steps/3/logs/stream?after=12",
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
      { credentials: "include" },
    );
  });

  it("lists artifacts with project and job scope params", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue({
      ok: true,
      json: async () => ({
        data: {
          artifacts: [],
        },
      }),
    } as Response);

    await listArtifacts({
      project_id: "project-1",
      job_id: "job-1",
    });

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/artifacts?project_id=project-1&job_id=job-1",
      { credentials: "include" },
    );
  });

  it("lists artifact catalog entries with project, job, build, and pagination params", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue({
      ok: true,
      json: async () => ({
        data: {
          artifacts: [],
        },
      }),
    } as Response);

    await listArtifactCatalog({
      q: "pkg",
      project_id: "project-1",
      job_id: "job-1",
      build_id: "build-1",
      limit: 5,
      offset: 10,
    });

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/artifacts/catalog?q=pkg&project_id=project-1&job_id=job-1&build_id=build-1&limit=5&offset=10",
      { credentials: "include" },
    );
  });

  it("lists artifact catalog entries without optional params", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue({
      ok: true,
      json: async () => ({
        data: {
          artifacts: [],
        },
      }),
    } as Response);

    await listArtifactCatalog();

    expect(fetchMock).toHaveBeenCalledWith("/api/artifacts/catalog", {
      credentials: "include",
    });
  });

  it("trims project slug filters for artifact browse and catalog requests", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue({
      ok: true,
      json: async () => ({
        data: {
          artifacts: [],
        },
      }),
    } as Response);

    await listArtifacts({ q: "  pkg  ", project_slug: "  platform  " });
    await listArtifactCatalog({ project_slug: "  platform  " });

    expect(fetchMock).toHaveBeenNthCalledWith(
      1,
      "/api/artifacts?q=pkg&project_slug=platform",
      { credentials: "include" },
    );
    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      "/api/artifacts/catalog?project_slug=platform",
      { credentials: "include" },
    );
  });

  it("fetches artifact detail from /artifacts/{id}", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue({
      ok: true,
      json: async () => ({
        data: {
          id: "artifact-1",
          build_id: "build-1",
          build_number: 41,
          build_status: "success",
          project_id: "project-1",
          path: "dist/app.tar",
          artifact_type: "generic",
          size_bytes: 128,
          content_type: null,
          checksum_sha256: null,
          storage_provider: "filesystem",
          download_url_path: "/builds/build-1/artifacts/artifact-1/download",
          version_tags: [
            {
              id: "tag-1",
              job_id: "job-1",
              kind: "version",
              version: "1.2.3",
              target_type: "artifact",
              artifact_id: "artifact-1",
              created_at: "2026-03-24T00:00:02Z",
            },
          ],
          created_at: "2026-03-24T00:00:01Z",
        },
      }),
    } as Response);

    const artifact = await getArtifact("artifact-1");
    expect(artifact.id).toBe("artifact-1");
    expect(artifact.version_tags).toEqual([
      {
        id: "tag-1",
        job_id: "job-1",
        kind: "version",
        version: "1.2.3",
        target_type: "artifact",
        artifact_id: "artifact-1",
        created_at: "2026-03-24T00:00:02Z",
      },
    ]);
    expect(fetchMock).toHaveBeenCalledWith("/api/artifacts/artifact-1", {
      credentials: "include",
    });
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
              kind: "version",
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
      credentials: "include",
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
    expect(fetchMock).toHaveBeenCalledWith("/api/jobs", {
      credentials: "include",
    });
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
    expect(fetchMock).toHaveBeenNthCalledWith(1, "/api/projects", {
      credentials: "include",
    });
    expect(fetchMock).toHaveBeenNthCalledWith(2, "/api/projects", {
      credentials: "include",
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        name: "Release",
        slug: "release",
        description: "Release automation",
      }),
    });
  });

  it("manages API tokens through /me/tokens", async () => {
    const fetchMock = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          data: {
            tokens: [
              {
                id: "token-1",
                name: "fixtures",
                token_prefix: "coyote_pat_abcd1234",
                created_at: "2026-05-12T00:00:00Z",
              },
            ],
          },
        }),
      } as Response)
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          data: {
            id: "token-2",
            name: "cli",
            token_prefix: "coyote_pat_efgh5678",
            token: "coyote_pat_raw",
            created_at: "2026-05-12T00:00:00Z",
          },
        }),
      } as Response)
      .mockResolvedValueOnce({ ok: true } as Response);

    const tokens = await listAPITokens();
    const created = await createAPIToken({ name: "cli" });
    await revokeAPIToken("token-1");

    expect(tokens).toHaveLength(1);
    expect(created.token).toBe("coyote_pat_raw");
    expect(fetchMock).toHaveBeenNthCalledWith(1, "/api/me/tokens", {
      credentials: "include",
    });
    expect(fetchMock).toHaveBeenNthCalledWith(2, "/api/me/tokens", {
      credentials: "include",
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name: "cli" }),
    });
    expect(fetchMock).toHaveBeenNthCalledWith(3, "/api/me/tokens/token-1", {
      credentials: "include",
      method: "DELETE",
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
      credentials: "include",
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
      credentials: "include",
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
      credentials: "include",
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
    expect(fetchMock).toHaveBeenCalledWith("/api/source-credentials", {
      credentials: "include",
    });
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
    expect(fetchMock).toHaveBeenCalledWith("/api/me", {
      credentials: "include",
    });
  });

  it("fetches public auth config from /auth/config", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue({
      ok: true,
      json: async () => ({
        data: {
          auth_mode: "oidc",
          login_url: "/auth/login",
        },
      }),
    } as Response);

    const config = await getAuthConfig();
    expect(config.auth_mode).toBe("oidc");
    expect(config.login_url).toBe("/auth/login");
    expect(fetchMock).toHaveBeenCalledWith("/api/auth/config", {
      credentials: "include",
    });
  });

  it("uses auth login and logout endpoints outside /api", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue({
      ok: true,
      text: async () => "",
    } as Response);

    expect(authLoginURL()).toBe("/auth/login");
    await logoutSession();

    expect(fetchMock).toHaveBeenCalledWith("/auth/logout", {
      credentials: "include",
      method: "POST",
      headers: { Accept: "application/json" },
    });
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

    expect(fetchMock).toHaveBeenNthCalledWith(1, "/api/users", {
      credentials: "include",
    });
    expect(fetchMock).toHaveBeenNthCalledWith(2, "/api/users", {
      credentials: "include",
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ email: "dev@example.com", global_role: "user" }),
    });
    expect(fetchMock).toHaveBeenNthCalledWith(3, "/api/users/user-1", {
      credentials: "include",
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ global_role: "admin" }),
    });
    expect(fetchMock).toHaveBeenNthCalledWith(4, "/api/users/user-1", {
      credentials: "include",
      method: "DELETE",
    });
    expect(fetchMock).toHaveBeenNthCalledWith(
      5,
      "/api/projects/project-1/members",
      { credentials: "include" },
    );
    expect(fetchMock).toHaveBeenNthCalledWith(
      6,
      "/api/projects/project-1/members/user-1",
      {
        credentials: "include",
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ role: "viewer" }),
      },
    );
    expect(fetchMock).toHaveBeenNthCalledWith(
      7,
      "/api/projects/project-1/members/user-1",
      {
        credentials: "include",
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ role: "owner" }),
      },
    );
    expect(fetchMock).toHaveBeenNthCalledWith(
      8,
      "/api/projects/project-1/members/user-1",
      { credentials: "include", method: "DELETE" },
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
        status: 404,
        text: async () =>
          JSON.stringify({
            error: {
              code: "artifact_not_found",
              message: "artifact not found",
            },
          }),
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
    await expect(getArtifact("artifact-1")).rejects.toMatchObject({
      name: "APIError",
      status: 404,
      code: "artifact_not_found",
      message: "API 404: artifact not found",
    });
    await expect(deleteUser("user-1")).rejects.toMatchObject({
      name: "APIError",
      status: 500,
      message: "API 500: internal server error",
    });
  });
});
