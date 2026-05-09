import type {
  Build,
  BuildArtifact,
  BuildArtifactsResponse,
  BuildListResponse,
  BuildStep,
  BuildStepsResponse,
  DataEnvelope,
  JobVersionTagsResponse,
  QueueEntry,
  QueueListResponse,
  StepLogsResponse,
  VersionTag,
  VersionTagCreateRequest,
} from "../types/build";
import type {
  ArtifactBrowseItem,
  ArtifactBrowseResponse,
  ArtifactType,
  DataEnvelope as ArtifactEnvelope,
} from "../types/artifact";
import type {
  CreateJobRequest,
  Job,
  JobListResponse,
  UpdateJobRequest,
} from "../types/job";
import type {
  CreateSourceCredentialRequest,
  SourceCredential,
  SourceCredentialListResponse,
  UpdateSourceCredentialRequest,
} from "../types/managedImageSettings";
import type {
  CreateProjectRequest,
  Project,
  ProjectJobsResponse,
  ProjectListResponse,
  UpdateProjectRequest,
} from "../types/project";
import type {
  CreateUserRequest,
  MeResponse,
  ProjectMember,
  ProjectMemberListResponse,
  ProjectMemberRole,
  UpdateUserRequest,
  UpsertProjectMemberRequest,
  User,
  UserListResponse,
} from "../types/identity";

/**
 * Base URL for API requests.
 *
 * In Docker (production-like), the nginx reverse-proxy exposes the backend at /api.
 * In local Vite dev, the Vite proxy forwards /api/* to the backend target.
 * Override with VITE_API_BASE_PATH when needed (e.g. direct backend testing).
 */
const BASE = import.meta.env.VITE_API_BASE_PATH ?? "/api";
const AUTH_BASE =
  import.meta.env.VITE_AUTH_BASE_PATH ?? BASE.replace(/\/api\/?$/, "");

export class APIError extends Error {
  status: number;

  constructor(status: number, message: string) {
    super(`API ${status}: ${message}`);
    this.name = "APIError";
    this.status = status;
  }
}

export function isAPIErrorStatus(error: unknown, status: number): boolean {
  return error instanceof APIError && error.status === status;
}

export function formatAPIErrorMessage(
  error: unknown,
  forbiddenFallback: string,
  operationPrefix?: string,
): string {
  if (error instanceof APIError) {
    if (error.status === 401) {
      return "Coyote is configured for external authentication. Sign in through the configured gateway or proxy, then retry.";
    }
    if (error.status === 403) {
      return forbiddenFallback;
    }
  }
  const message = error instanceof Error ? error.message : String(error);
  if (operationPrefix) {
    return `${operationPrefix}: ${message}`;
  }
  return message;
}

export async function checkReadiness(): Promise<void> {
  const res = await fetch(`${BASE}/readyz`);
  if (!res.ok) {
    throw new Error(`API ${res.status}: backend not ready`);
  }
}

async function fetchJSON<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${BASE}${path}`, init);
  if (!res.ok) {
    const body = await res.text();
    let message = body;

    try {
      const parsed = JSON.parse(body) as { error?: { message?: string } };
      if (parsed?.error?.message) {
        message = parsed.error.message;
      }
    } catch {
      // Keep raw body when response is not JSON.
    }

    throw new APIError(res.status, message);
  }
  return res.json() as Promise<T>;
}

async function postJSON<TResponse, TRequest>(
  path: string,
  body: TRequest,
): Promise<TResponse> {
  return fetchJSON<TResponse>(path, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(body),
  });
}

async function postNoBodyJSON<TResponse>(path: string): Promise<TResponse> {
  return fetchJSON<TResponse>(path, {
    method: "POST",
  });
}

async function deleteNoContent(path: string): Promise<void> {
  const res = await fetch(`${BASE}${path}`, { method: "DELETE" });
  if (!res.ok) {
    const body = await res.text();
    let message = body;

    try {
      const parsed = JSON.parse(body) as { error?: { message?: string } };
      if (parsed?.error?.message) {
        message = parsed.error.message;
      }
    } catch {
      // Keep raw body when response is not JSON.
    }

    throw new APIError(res.status, message);
  }
}

export async function getMe(): Promise<MeResponse> {
  const envelope = await fetchJSON<DataEnvelope<MeResponse>>("/me");
  return envelope.data;
}

export function authLoginURL(): string {
  return `${AUTH_BASE}/auth/login`;
}

export async function logoutSession(): Promise<void> {
  const res = await fetch(`${AUTH_BASE}/auth/logout`, {
    method: "POST",
    headers: {
      Accept: "application/json",
    },
  });
  if (!res.ok) {
    const body = await res.text();
    throw new APIError(res.status, body || "logout failed");
  }
}

export async function listUsers(): Promise<User[]> {
  const envelope = await fetchJSON<DataEnvelope<UserListResponse>>("/users");
  return envelope.data.users;
}

export async function createUser(input: CreateUserRequest): Promise<User> {
  const envelope = await postJSON<DataEnvelope<User>, CreateUserRequest>(
    "/users",
    input,
  );
  return envelope.data;
}

export async function updateUser(
  id: string,
  input: UpdateUserRequest,
): Promise<User> {
  const envelope = await fetchJSON<DataEnvelope<User>>(
    `/users/${encodeURIComponent(id)}`,
    {
      method: "PATCH",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(input),
    },
  );
  return envelope.data;
}

export async function deleteUser(id: string): Promise<void> {
  await deleteNoContent(`/users/${encodeURIComponent(id)}`);
}

export async function listProjectMembers(
  projectId: string,
): Promise<ProjectMember[]> {
  const envelope = await fetchJSON<DataEnvelope<ProjectMemberListResponse>>(
    `/projects/${encodeURIComponent(projectId)}/members`,
  );
  return envelope.data.members;
}

export async function upsertProjectMember(
  projectId: string,
  userId: string,
  role: ProjectMemberRole,
): Promise<ProjectMember> {
  const envelope = await fetchJSON<DataEnvelope<ProjectMember>>(
    `/projects/${encodeURIComponent(projectId)}/members/${encodeURIComponent(userId)}`,
    {
      method: "PUT",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify({ role } satisfies UpsertProjectMemberRequest),
    },
  );
  return envelope.data;
}

export async function updateProjectMember(
  projectId: string,
  userId: string,
  role: ProjectMemberRole,
): Promise<ProjectMember> {
  const envelope = await fetchJSON<DataEnvelope<ProjectMember>>(
    `/projects/${encodeURIComponent(projectId)}/members/${encodeURIComponent(userId)}`,
    {
      method: "PATCH",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify({ role } satisfies UpsertProjectMemberRequest),
    },
  );
  return envelope.data;
}

export async function deleteProjectMember(
  projectId: string,
  userId: string,
): Promise<void> {
  await deleteNoContent(
    `/projects/${encodeURIComponent(projectId)}/members/${encodeURIComponent(userId)}`,
  );
}

export async function listBuilds(input?: {
  project_id?: string;
  project_slug?: string;
  limit?: number;
  offset?: number;
}): Promise<Build[]> {
  const params = new URLSearchParams();
  const projectID = input?.project_id?.trim() ?? "";
  const projectSlug = input?.project_slug?.trim() ?? "";

  if (projectID) {
    params.set("project_id", projectID);
  }
  if (projectSlug) {
    params.set("project_slug", projectSlug);
  }
  if (typeof input?.limit === "number" && input.limit > 0) {
    params.set("limit", String(input.limit));
  }
  if (typeof input?.offset === "number" && input.offset > 0) {
    params.set("offset", String(input.offset));
  }

  const suffix = params.size > 0 ? `?${params.toString()}` : "";
  const envelope = await fetchJSON<DataEnvelope<BuildListResponse>>(
    `/builds${suffix}`,
  );
  return envelope.data.builds;
}

export async function getBuild(id: string): Promise<Build> {
  const envelope = await fetchJSON<DataEnvelope<Build>>(
    `/builds/${encodeURIComponent(id)}`,
  );
  return envelope.data;
}

export async function listQueue(input?: {
  project_id?: string;
  project_slug?: string;
  status?: "queued" | "running";
}): Promise<QueueEntry[]> {
  const params = new URLSearchParams();
  const projectID = input?.project_id?.trim() ?? "";
  const projectSlug = input?.project_slug?.trim() ?? "";
  const status = input?.status?.trim() ?? "";

  if (projectID) {
    params.set("project_id", projectID);
  }
  if (projectSlug) {
    params.set("project_slug", projectSlug);
  }
  if (status) {
    params.set("status", status);
  }

  const suffix = params.size > 0 ? `?${params.toString()}` : "";
  const envelope = await fetchJSON<DataEnvelope<QueueListResponse>>(
    `/queue${suffix}`,
  );
  return envelope.data.entries;
}

export async function getBuildSteps(id: string): Promise<BuildStep[]> {
  const envelope = await fetchJSON<DataEnvelope<BuildStepsResponse>>(
    `/builds/${encodeURIComponent(id)}/steps`,
  );
  return envelope.data.steps;
}

export async function getStepLogs(
  buildID: string,
  stepIndex: number,
  after = 0,
  limit = 300,
): Promise<StepLogsResponse> {
  const envelope = await fetchJSON<DataEnvelope<StepLogsResponse>>(
    `/builds/${encodeURIComponent(buildID)}/steps/${stepIndex}/logs?after=${after}&limit=${limit}`,
  );
  return envelope.data;
}

export async function getBuildArtifacts(id: string): Promise<BuildArtifact[]> {
  const envelope = await fetchJSON<DataEnvelope<BuildArtifactsResponse>>(
    `/builds/${encodeURIComponent(id)}/artifacts`,
  );
  return envelope.data.artifacts;
}

export async function listArtifacts(input?: {
  q?: string;
  type?: ArtifactType | "";
  project_id?: string;
  project_slug?: string;
  limit?: number;
  offset?: number;
}): Promise<ArtifactBrowseItem[]> {
  const params = new URLSearchParams();
  const query = input?.q?.trim() ?? "";
  const type = input?.type?.trim() ?? "";
  const projectID = input?.project_id?.trim() ?? "";
  const projectSlug = input?.project_slug?.trim() ?? "";

  if (query) {
    params.set("q", query);
  }
  if (type) {
    params.set("type", type);
  }
  if (projectID) {
    params.set("project_id", projectID);
  }
  if (projectSlug) {
    params.set("project_slug", projectSlug);
  }
  if (typeof input?.limit === "number" && input.limit > 0) {
    params.set("limit", String(input.limit));
  }
  if (typeof input?.offset === "number" && input.offset > 0) {
    params.set("offset", String(input.offset));
  }

  const suffix = params.size > 0 ? `?${params.toString()}` : "";
  const envelope = await fetchJSON<ArtifactEnvelope<ArtifactBrowseResponse>>(
    `/artifacts${suffix}`,
  );
  return envelope.data.artifacts;
}

export async function createJobVersionTags(
  jobID: string,
  input: VersionTagCreateRequest,
): Promise<VersionTag[]> {
  const envelope = await postJSON<
    DataEnvelope<JobVersionTagsResponse>,
    VersionTagCreateRequest
  >(`/jobs/${encodeURIComponent(jobID)}/version-tags`, input);
  return envelope.data.tags;
}

export function artifactDownloadURL(downloadPath: string): string {
  if (!downloadPath.startsWith("/")) {
    return `${BASE}/${downloadPath}`;
  }
  return `${BASE}${downloadPath}`;
}

export function buildStepLogStreamURL(
  buildID: string,
  stepIndex: number,
  after = 0,
): string {
  return `${BASE}/builds/${encodeURIComponent(buildID)}/steps/${stepIndex}/logs/stream?after=${after}`;
}

export async function listJobs(): Promise<Job[]> {
  const envelope = await fetchJSON<DataEnvelope<JobListResponse>>("/jobs");
  return envelope.data.jobs;
}

export async function listProjects(): Promise<Project[]> {
  const envelope =
    await fetchJSON<DataEnvelope<ProjectListResponse>>("/projects");
  return envelope.data.projects;
}

export async function getProject(id: string): Promise<Project> {
  const envelope = await fetchJSON<DataEnvelope<Project>>(
    `/projects/${encodeURIComponent(id)}`,
  );
  return envelope.data;
}

export async function createProject(
  input: CreateProjectRequest,
): Promise<Project> {
  const envelope = await postJSON<DataEnvelope<Project>, CreateProjectRequest>(
    "/projects",
    input,
  );
  return envelope.data;
}

export async function updateProject(
  id: string,
  input: UpdateProjectRequest,
): Promise<Project> {
  const envelope = await fetchJSON<DataEnvelope<Project>>(
    `/projects/${encodeURIComponent(id)}`,
    {
      method: "PATCH",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(input),
    },
  );
  return envelope.data;
}

export async function deleteProject(id: string): Promise<void> {
  await deleteNoContent(`/projects/${encodeURIComponent(id)}`);
}

export async function listJobsByProject(projectId: string): Promise<Job[]> {
  const envelope = await fetchJSON<DataEnvelope<ProjectJobsResponse>>(
    `/projects/${encodeURIComponent(projectId)}/jobs`,
  );
  return envelope.data.jobs;
}

export async function getJob(id: string): Promise<Job> {
  const envelope = await fetchJSON<DataEnvelope<Job>>(
    `/jobs/${encodeURIComponent(id)}`,
  );
  return envelope.data;
}

export async function createJob(input: CreateJobRequest): Promise<Job> {
  const envelope = await postJSON<DataEnvelope<Job>, CreateJobRequest>(
    "/jobs",
    input,
  );
  return envelope.data;
}

export async function updateJob(
  id: string,
  input: UpdateJobRequest,
): Promise<Job> {
  const envelope = await fetchJSON<DataEnvelope<Job>>(
    `/jobs/${encodeURIComponent(id)}`,
    {
      method: "PUT",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(input),
    },
  );
  return envelope.data;
}

export async function runJob(id: string): Promise<Build> {
  const envelope = await postNoBodyJSON<DataEnvelope<Build>>(
    `/jobs/${encodeURIComponent(id)}/run`,
  );
  return envelope.data;
}

export async function listBuildsByJob(jobId: string): Promise<Build[]> {
  const envelope = await fetchJSON<DataEnvelope<BuildListResponse>>(
    `/jobs/${encodeURIComponent(jobId)}/builds`,
  );
  return envelope.data.builds;
}

export async function listSourceCredentials(): Promise<SourceCredential[]> {
  const envelope = await fetchJSON<DataEnvelope<SourceCredentialListResponse>>(
    "/source-credentials",
  );
  return envelope.data.credentials;
}

export async function createSourceCredential(
  input: CreateSourceCredentialRequest,
): Promise<SourceCredential> {
  const envelope = await postJSON<
    DataEnvelope<SourceCredential>,
    CreateSourceCredentialRequest
  >("/source-credentials", input);
  return envelope.data;
}

export async function updateSourceCredential(
  id: string,
  input: UpdateSourceCredentialRequest,
): Promise<SourceCredential> {
  const envelope = await fetchJSON<DataEnvelope<SourceCredential>>(
    `/source-credentials/${encodeURIComponent(id)}`,
    {
      method: "PUT",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(input),
    },
  );
  return envelope.data;
}

export async function deleteSourceCredential(id: string): Promise<void> {
  await deleteNoContent(`/source-credentials/${encodeURIComponent(id)}`);
}
