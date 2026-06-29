import type { Build, BuildListResponse, DataEnvelope } from "../types/build";
import type {
  ArtifactCatalogItem,
  ArtifactCatalogResponse,
  ArtifactDetail,
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
import type { Worker, WorkerListResponse } from "../types/worker";
import type {
  CreateProjectRequest,
  Project,
  ProjectJobsResponse,
  ProjectListResponse,
  UpdateProjectRequest,
} from "../types/project";
import type {
  CreateUserRequest,
  APIToken,
  APITokenListResponse,
  CreateAPITokenRequest,
  CreatedAPIToken,
  AuthConfigResponse,
  MeResponse,
  ProjectMember,
  ProjectMemberListResponse,
  ProjectMemberRole,
  UpdateUserRequest,
  UpsertProjectMemberRequest,
  User,
  UserListResponse,
} from "../types/identity";
import type {
  CommitAuthorFailureNotificationPreference,
  CreateNotificationSubscriptionRequest,
  CreateNotificationTargetRequest,
  MyEmailNotificationTargetResponse,
  NotificationDefaults,
  NotificationSubscription,
  NotificationSubscriptionListResponse,
  NotificationTarget,
  NotificationTargetListResponse,
  UpdateCommitAuthorFailureNotificationPreferenceRequest,
  UpdateNotificationDefaultsRequest,
  UpdateNotificationSubscriptionRequest,
  UpdateNotificationTargetRequest,
} from "../types/notification";
import {
  APIError,
  AUTH_BASE,
  BASE,
  deleteNoContent,
  fetchJSON,
  postJSON,
  postNoBodyJSON,
  withCredentials,
} from "./request";
export { APIError } from "./request";
export {
  artifactDownloadURL,
  buildStepLogStreamURL,
  cancelBuild,
  createJobVersionTags,
  getBuild,
  getBuildArtifacts,
  getBuildSteps,
  getStepLogs,
  listBuilds,
  listQueue,
  rerunBuild,
} from "./buildClient";

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

export async function getMe(): Promise<MeResponse> {
  const envelope = await fetchJSON<DataEnvelope<MeResponse>>("/me");
  return envelope.data;
}

export async function getAuthConfig(): Promise<AuthConfigResponse> {
  const envelope =
    await fetchJSON<DataEnvelope<AuthConfigResponse>>("/auth/config");
  return envelope.data;
}

export function authLoginURL(): string {
  return `${AUTH_BASE}/auth/login`;
}

export async function logoutSession(): Promise<void> {
  const res = await fetch(
    `${AUTH_BASE}/auth/logout`,
    withCredentials({
      method: "POST",
      headers: {
        Accept: "application/json",
      },
    }),
  );
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

export async function listAPITokens(): Promise<APIToken[]> {
  const envelope =
    await fetchJSON<DataEnvelope<APITokenListResponse>>("/me/tokens");
  return envelope.data.tokens;
}

export async function createAPIToken(
  input: CreateAPITokenRequest,
): Promise<CreatedAPIToken> {
  const envelope = await postJSON<
    DataEnvelope<CreatedAPIToken>,
    CreateAPITokenRequest
  >("/me/tokens", input);
  return envelope.data;
}

export async function revokeAPIToken(id: string): Promise<void> {
  await deleteNoContent(`/me/tokens/${encodeURIComponent(id)}`);
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

export async function listNotificationTargets(): Promise<NotificationTarget[]> {
  const envelope = await fetchJSON<
    DataEnvelope<NotificationTargetListResponse>
  >("/notification-targets");
  return envelope.data.targets;
}

export async function getMyEmailNotificationTarget(): Promise<NotificationTarget | null> {
  const envelope = await fetchJSON<
    DataEnvelope<MyEmailNotificationTargetResponse>
  >("/me/notification-targets/email");
  return envelope.data.target;
}

export async function ensureMyEmailNotificationTarget(): Promise<NotificationTarget> {
  const envelope = await postNoBodyJSON<DataEnvelope<NotificationTarget>>(
    "/me/notification-targets/email",
  );
  return envelope.data;
}

export async function getCommitAuthorFailureNotificationPreference(): Promise<CommitAuthorFailureNotificationPreference> {
  const envelope = await fetchJSON<
    DataEnvelope<CommitAuthorFailureNotificationPreference>
  >("/me/notification-preferences/commit-author-failures");
  return envelope.data;
}

export async function getNotificationDefaults(): Promise<NotificationDefaults> {
  const envelope = await fetchJSON<DataEnvelope<NotificationDefaults>>(
    "/settings/notifications/defaults",
  );
  return envelope.data;
}

export async function setCommitAuthorFailureNotificationPreference(
  input: UpdateCommitAuthorFailureNotificationPreferenceRequest,
): Promise<CommitAuthorFailureNotificationPreference> {
  const envelope = await fetchJSON<
    DataEnvelope<CommitAuthorFailureNotificationPreference>
  >("/me/notification-preferences/commit-author-failures", {
    method: "PUT",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(input),
  });
  return envelope.data;
}

export async function setNotificationDefaults(
  input: UpdateNotificationDefaultsRequest,
): Promise<NotificationDefaults> {
  const envelope = await fetchJSON<DataEnvelope<NotificationDefaults>>(
    "/settings/notifications/defaults",
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

export async function createNotificationTarget(
  input: CreateNotificationTargetRequest,
): Promise<NotificationTarget> {
  const envelope = await postJSON<
    DataEnvelope<NotificationTarget>,
    CreateNotificationTargetRequest
  >("/notification-targets", input);
  return envelope.data;
}

export async function updateNotificationTarget(
  id: string,
  input: UpdateNotificationTargetRequest,
): Promise<NotificationTarget> {
  const envelope = await fetchJSON<DataEnvelope<NotificationTarget>>(
    `/notification-targets/${encodeURIComponent(id)}`,
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

export async function listNotificationSubscriptions(input?: {
  project_id?: string;
  job_id?: string;
}): Promise<NotificationSubscription[]> {
  const params = new URLSearchParams();
  const projectID = input?.project_id?.trim() ?? "";
  const jobID = input?.job_id?.trim() ?? "";

  if (projectID) {
    params.set("project_id", projectID);
  }
  if (jobID) {
    params.set("job_id", jobID);
  }

  const suffix = params.size > 0 ? `?${params.toString()}` : "";
  const envelope = await fetchJSON<
    DataEnvelope<NotificationSubscriptionListResponse>
  >(`/notification-subscriptions${suffix}`);
  return envelope.data.subscriptions;
}

export async function createNotificationSubscription(
  input: CreateNotificationSubscriptionRequest,
): Promise<NotificationSubscription> {
  const envelope = await postJSON<
    DataEnvelope<NotificationSubscription>,
    CreateNotificationSubscriptionRequest
  >("/notification-subscriptions", input);
  return envelope.data;
}

export async function updateNotificationSubscription(
  id: string,
  input: UpdateNotificationSubscriptionRequest,
): Promise<NotificationSubscription> {
  const envelope = await fetchJSON<DataEnvelope<NotificationSubscription>>(
    `/notification-subscriptions/${encodeURIComponent(id)}`,
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

export async function deleteNotificationSubscription(
  id: string,
): Promise<void> {
  await deleteNoContent(
    `/notification-subscriptions/${encodeURIComponent(id)}`,
  );
}

export async function listWorkers(): Promise<Worker[]> {
  const envelope =
    await fetchJSON<DataEnvelope<WorkerListResponse>>("/workers");
  return envelope.data.workers;
}

export async function listArtifacts(input?: {
  q?: string;
  type?: ArtifactType | "";
  project_id?: string;
  job_id?: string;
  project_slug?: string;
  limit?: number;
  offset?: number;
}): Promise<ArtifactBrowseItem[]> {
  const params = new URLSearchParams();
  const query = input?.q?.trim() ?? "";
  const type = input?.type?.trim() ?? "";
  const projectID = input?.project_id?.trim() ?? "";
  const jobID = input?.job_id?.trim() ?? "";
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
  if (jobID) {
    params.set("job_id", jobID);
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

export async function listArtifactCatalog(input?: {
  q?: string;
  project_id?: string;
  project_slug?: string;
  job_id?: string;
  build_id?: string;
  limit?: number;
  offset?: number;
}): Promise<ArtifactCatalogItem[]> {
  const params = new URLSearchParams();
  const query = input?.q?.trim() ?? "";
  const projectID = input?.project_id?.trim() ?? "";
  const projectSlug = input?.project_slug?.trim() ?? "";
  const jobID = input?.job_id?.trim() ?? "";
  const buildID = input?.build_id?.trim() ?? "";

  if (query) {
    params.set("q", query);
  }
  if (projectID) {
    params.set("project_id", projectID);
  }
  if (projectSlug) {
    params.set("project_slug", projectSlug);
  }
  if (jobID) {
    params.set("job_id", jobID);
  }
  if (buildID) {
    params.set("build_id", buildID);
  }
  if (typeof input?.limit === "number" && input.limit > 0) {
    params.set("limit", String(input.limit));
  }
  if (typeof input?.offset === "number" && input.offset > 0) {
    params.set("offset", String(input.offset));
  }

  const suffix = params.size > 0 ? `?${params.toString()}` : "";
  const envelope = await fetchJSON<ArtifactEnvelope<ArtifactCatalogResponse>>(
    `/artifacts/catalog${suffix}`,
  );
  return envelope.data.artifacts;
}

export async function getArtifact(id: string): Promise<ArtifactDetail> {
  const envelope = await fetchJSON<ArtifactEnvelope<ArtifactDetail>>(
    `/artifacts/${encodeURIComponent(id)}`,
  );
  return envelope.data;
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
