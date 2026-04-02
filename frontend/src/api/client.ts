import type {
  Build,
  BuildArtifact,
  BuildArtifactsResponse,
  BuildTemplate,
  BuildListResponse,
  BuildStep,
  BuildStepsResponse,
  CreateBuildRequest,
  CreatePipelineBuildRequest,
  CreateRepoBuildRequest,
  DataEnvelope,
  QueueBuildStepInput,
  RerunBuildFromStepRequest,
  RetryJobResponse,
  StepLogsResponse,
} from '../types/build';
import type {
  CreateJobRequest,
  Job,
  JobListResponse,
  UpdateJobRequest,
} from '../types/job';

/**
 * Base URL for API requests.
 *
 * In Docker (production-like), the nginx reverse-proxy exposes the backend at /api.
 * In local Vite dev, the vite proxy rewrites /api -> http://localhost:8080.
 * Override with VITE_API_BASE_PATH when needed (e.g. direct backend testing).
 */
const BASE = import.meta.env.VITE_API_BASE_PATH ?? '/api';

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

async function postJSON<TResponse, TRequest>(path: string, body: TRequest): Promise<TResponse> {
  return fetchJSON<TResponse>(path, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(body),
  });
}

async function postNoBodyJSON<TResponse>(path: string): Promise<TResponse> {
  return fetchJSON<TResponse>(path, {
    method: 'POST',
  });
}

export async function listBuilds(): Promise<Build[]> {
  const envelope = await fetchJSON<DataEnvelope<BuildListResponse>>('/builds');
  return envelope.data.builds;
}

export async function getBuild(id: string): Promise<Build> {
  const envelope = await fetchJSON<DataEnvelope<Build>>(`/builds/${encodeURIComponent(id)}`);
  return envelope.data;
}

export async function getBuildSteps(id: string): Promise<BuildStep[]> {
  const envelope = await fetchJSON<DataEnvelope<BuildStepsResponse>>(
    `/builds/${encodeURIComponent(id)}/steps`,
  );
  return envelope.data.steps;
}

export async function getStepLogs(buildID: string, stepIndex: number, after = 0, limit = 300): Promise<StepLogsResponse> {
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

export function artifactDownloadURL(downloadPath: string): string {
  if (!downloadPath.startsWith('/')) {
    return `${BASE}/${downloadPath}`;
  }
  return `${BASE}${downloadPath}`;
}

export function buildStepLogStreamURL(buildID: string, stepIndex: number, after = 0): string {
  return `${BASE}/builds/${encodeURIComponent(buildID)}/steps/${stepIndex}/logs/stream?after=${after}`;
}

export async function createBuild(input: CreateBuildRequest): Promise<Build> {
  const envelope = await postJSON<DataEnvelope<Build>, CreateBuildRequest>('/builds', input);
  return envelope.data;
}

export async function createPipelineBuild(input: CreatePipelineBuildRequest): Promise<Build> {
  const envelope = await postJSON<DataEnvelope<Build>, CreatePipelineBuildRequest>('/builds/pipeline', input);
  return envelope.data;
}

export async function createRepoBuild(input: CreateRepoBuildRequest): Promise<Build> {
  const envelope = await postJSON<DataEnvelope<Build>, CreateRepoBuildRequest>('/builds/repo', input);
  return envelope.data;
}

export async function queueBuild(id: string, template?: BuildTemplate, steps?: QueueBuildStepInput[]): Promise<Build> {
  const path = `/builds/${encodeURIComponent(id)}/queue`;
  const shouldSendBody = Boolean(template) || Boolean(steps && steps.length > 0);
  const envelope = shouldSendBody
    ? await postJSON<DataEnvelope<Build>, { template?: BuildTemplate; steps?: QueueBuildStepInput[] }>(
        path,
        {
          ...(template ? { template } : {}),
          ...(steps && steps.length > 0 ? { steps } : {}),
        },
      )
    : await postNoBodyJSON<DataEnvelope<Build>>(path);
  return envelope.data;
}

export async function retryFailedJob(jobID: string): Promise<RetryJobResponse> {
  const envelope = await postNoBodyJSON<DataEnvelope<RetryJobResponse>>(
    `/builds/jobs/${encodeURIComponent(jobID)}/retry`,
  );
  return envelope.data;
}

export async function rerunBuildFromStep(buildID: string, stepIndex: number): Promise<Build> {
  const envelope = await postJSON<DataEnvelope<Build>, RerunBuildFromStepRequest>(
    `/builds/${encodeURIComponent(buildID)}/rerun`,
    { step_index: stepIndex },
  );
  return envelope.data;
}

export async function listJobs(): Promise<Job[]> {
  const envelope = await fetchJSON<DataEnvelope<JobListResponse>>('/jobs');
  return envelope.data.jobs;
}

export async function getJob(id: string): Promise<Job> {
  const envelope = await fetchJSON<DataEnvelope<Job>>(`/jobs/${encodeURIComponent(id)}`);
  return envelope.data;
}

export async function createJob(input: CreateJobRequest): Promise<Job> {
  const envelope = await postJSON<DataEnvelope<Job>, CreateJobRequest>('/jobs', input);
  return envelope.data;
}

export async function updateJob(id: string, input: UpdateJobRequest): Promise<Job> {
  const envelope = await fetchJSON<DataEnvelope<Job>>(`/jobs/${encodeURIComponent(id)}`, {
    method: 'PUT',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(input),
  });
  return envelope.data;
}

export async function runJob(id: string): Promise<Build> {
  const envelope = await postNoBodyJSON<DataEnvelope<Build>>(`/jobs/${encodeURIComponent(id)}/run`);
  return envelope.data;
}
