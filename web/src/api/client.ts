import type {
  Build,
  BuildListResponse,
  BuildStep,
  BuildStepsResponse,
  DataEnvelope,
} from '../types/build';

/**
 * Base URL for API requests.
 *
 * In Docker (production-like), the nginx reverse-proxy exposes the backend at /api.
 * In local Vite dev, the vite proxy rewrites /api -> http://localhost:8080.
 */
const BASE = '/api';

async function fetchJSON<T>(path: string): Promise<T> {
  const res = await fetch(`${BASE}${path}`);
  if (!res.ok) {
    const body = await res.text();
    throw new Error(`API ${res.status}: ${body}`);
  }
  return res.json() as Promise<T>;
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
