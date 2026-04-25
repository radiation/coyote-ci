import type {
  Build,
  BuildArtifact,
  BuildArtifactsResponse,
  BuildListResponse,
  BuildStep,
  BuildStepsResponse,
  DataEnvelope,
  JobVersionTagsResponse,
  StepLogsResponse,
  VersionTag,
  VersionTagCreateRequest,
} from "../types/build";
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

/**
 * Base URL for API requests.
 *
 * In Docker (production-like), the nginx reverse-proxy exposes the backend at /api.
 * In local Vite dev, the Vite proxy forwards /api/* to the backend target.
 * Override with VITE_API_BASE_PATH when needed (e.g. direct backend testing).
 */
const BASE = import.meta.env.VITE_API_BASE_PATH ?? "/api";

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

    throw new Error(`API ${res.status}: ${message}`);
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

    throw new Error(`API ${res.status}: ${message}`);
  }
}

export async function listBuilds(): Promise<Build[]> {
  const envelope = await fetchJSON<DataEnvelope<BuildListResponse>>("/builds");
  return envelope.data.builds;
}

export async function getBuild(id: string): Promise<Build> {
  const envelope = await fetchJSON<DataEnvelope<Build>>(
    `/builds/${encodeURIComponent(id)}`,
  );
  return envelope.data;
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
