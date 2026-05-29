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
import { BASE, fetchJSON, postJSON, postNoBodyJSON } from "./request";

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

export async function cancelBuild(id: string): Promise<Build> {
  const envelope = await postNoBodyJSON<DataEnvelope<Build>>(
    `/builds/${encodeURIComponent(id)}/cancel`,
  );
  return envelope.data;
}

export async function rerunBuild(id: string): Promise<Build> {
  const envelope = await postNoBodyJSON<DataEnvelope<Build>>(
    `/builds/${encodeURIComponent(id)}/rerun`,
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
