import type { BuildStatus } from "../types/build";

export const historyStatuses: BuildStatus[] = [
  "pending",
  "queued",
  "preparing",
  "running",
  "success",
  "failed",
  "canceled",
];

function integer(
  value: string | null,
  min: number,
  max: number,
  fallback: number,
) {
  const parsed = Number(value);
  return value && /^\d+$/.test(value) && parsed >= min && parsed <= max
    ? parsed
    : fallback;
}

export function readBuildHistory(params: URLSearchParams) {
  const project = params.get("project_id")?.trim() ?? "";
  return {
    project: /^[a-zA-Z0-9_-]{1,128}$/.test(project) ? project : "",
    status:
      historyStatuses.find((status) => status === params.get("status")) ?? "",
    limit: integer(params.get("limit"), 1, 200, 50),
    offset: integer(params.get("offset"), 0, Number.MAX_SAFE_INTEGER, 0),
  };
}

export function historyParams(history: ReturnType<typeof readBuildHistory>) {
  const params = new URLSearchParams();
  if (history.project) params.set("project_id", history.project);
  if (history.status) params.set("status", history.status);
  params.set("limit", String(history.limit));
  params.set("offset", String(history.offset));
  return params;
}

export function buildsReturnPath(params: URLSearchParams) {
  const origin = params.get("history");
  if (!origin) return "/builds";
  const history = readBuildHistory(new URLSearchParams(origin));
  return `/builds?${historyParams(history)}`;
}
