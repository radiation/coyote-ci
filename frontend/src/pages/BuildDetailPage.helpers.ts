import type { Build, BuildStep } from "../types";
import { isActiveBuild } from "../utils/build";
import { formatDuration } from "../utils/time";

function textValue(value: string | null | undefined): string | null {
  const trimmed = value?.trim();
  return trimmed ? trimmed : null;
}

function safeRepositoryURL(build: Build): URL | null {
  const candidate =
    textValue(build.repository_url) ?? textValue(build.source?.repository_url);
  if (!candidate) {
    return null;
  }

  try {
    const parsed = new URL(candidate);
    if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
      return null;
    }
    return parsed;
  } catch {
    return null;
  }
}

function githubRepositoryBaseURL(build: Build): string | null {
  const repositoryURL = safeRepositoryURL(build);
  if (!repositoryURL || repositoryURL.hostname.toLowerCase() !== "github.com") {
    return null;
  }

  const [owner, repoWithSuffix] = repositoryURL.pathname
    .split("/")
    .filter(Boolean);
  const repo = repoWithSuffix?.replace(/\.git$/i, "");

  if (!owner || !repo) {
    return null;
  }

  return `${repositoryURL.origin}/${encodeURIComponent(owner)}/${encodeURIComponent(repo)}`;
}

function encodePathSegments(value: string): string {
  return value
    .split("/")
    .filter(Boolean)
    .map((segment) => encodeURIComponent(segment))
    .join("/");
}

function normalizeGitHubRef(ref: string): string {
  if (ref.startsWith("refs/heads/")) {
    return ref.slice("refs/heads/".length);
  }
  if (ref.startsWith("refs/tags/")) {
    return ref.slice("refs/tags/".length);
  }

  return ref;
}

export function shortSHA(value: string | null | undefined): string {
  const trimmed = (value ?? "").trim();
  if (!trimmed) return "—";
  return trimmed.slice(0, 7);
}

export function triggerKind(build: Build): string {
  return (build.trigger_kind ?? "manual").trim() || "manual";
}

export function compactTriggerMetadata(build: Build): string {
  const parts: string[] = [];
  const provider = (build.scm_provider ?? "").trim();
  const ref = (build.trigger_ref ?? "").trim();
  const sha = (build.trigger_commit_sha ?? "").trim();
  const actor = (build.actor ?? "").trim();

  if (provider) parts.push(provider);
  if (ref) parts.push(ref);
  if (sha) parts.push(shortSHA(sha));
  if (actor) parts.push(actor);
  return parts.join(" • ");
}

export function buildLabel(build: Build): string {
  return build.build_number
    ? `Build #${build.build_number}`
    : `Build ${build.id.slice(0, 8)}`;
}

export function operationalBuildTitle(build: Build): string {
  const job = jobLabel(build);
  if (build.build_number) {
    return job !== "Manual"
      ? `${job} #${build.build_number}`
      : buildLabel(build);
  }

  return job !== "Manual"
    ? `${job} · ${build.id.slice(0, 8)}`
    : buildLabel(build);
}

export function projectLabel(build: Build): string {
  return (
    build.project_name?.trim() || build.project_slug?.trim() || build.project_id
  );
}

export function jobLabel(build: Build): string {
  const name = build.job_name?.trim();
  if (name) {
    return name;
  }

  const jobID = build.job_id?.trim();
  return jobID ? `Job ${jobID.slice(0, 8)}` : "Manual";
}

export function buildDuration(build: Build, buildUpdatedAt: number): string {
  const endISO =
    build.finished_at ??
    (isActiveBuild(build.status) && buildUpdatedAt > 0
      ? new Date(buildUpdatedAt).toISOString()
      : null);
  return formatDuration(build.started_at, endISO);
}

export function buildStepCounts(steps: BuildStep[] | undefined) {
  const counts = {
    total: 0,
    success: 0,
    failed: 0,
    canceled: 0,
    running: 0,
    pending: 0,
  };

  for (const step of steps ?? []) {
    counts.total += 1;
    counts[step.status] += 1;
  }

  return counts;
}

export function buildSourceRefValue(build: Build): string | null {
  return (
    textValue(build.source_ref) ??
    textValue(build.source?.ref) ??
    textValue(build.trigger_ref)
  );
}

export function buildPrimaryCommitValue(build: Build): string | null {
  return (
    textValue(build.source_sha) ??
    textValue(build.source_commit_sha) ??
    textValue(build.source?.source_commit_sha) ??
    textValue(build.trigger_commit_sha)
  );
}

export function buildGitHubCommitURL(
  build: Build,
  sha: string | null | undefined,
): string | null {
  const baseURL = githubRepositoryBaseURL(build);
  const commitSHA = textValue(sha);
  if (!baseURL || !commitSHA) {
    return null;
  }

  return `${baseURL}/commit/${encodeURIComponent(commitSHA)}`;
}

export function buildGitHubRefURL(
  build: Build,
  ref: string | null | undefined,
): string | null {
  const baseURL = githubRepositoryBaseURL(build);
  const refValue = textValue(ref);
  if (!baseURL || !refValue) {
    return null;
  }

  return `${baseURL}/tree/${encodePathSegments(normalizeGitHubRef(refValue))}`;
}

export function buildGitHubPipelinePathURL(build: Build): string | null {
  const baseURL = githubRepositoryBaseURL(build);
  const pipelinePath = textValue(build.pipeline_path);
  const revision =
    buildPrimaryCommitValue(build) ??
    (() => {
      const refValue = buildSourceRefValue(build);
      return refValue ? normalizeGitHubRef(refValue) : null;
    })();
  if (!baseURL || !pipelinePath || !revision) {
    return null;
  }

  return `${baseURL}/blob/${encodePathSegments(revision)}/${encodePathSegments(pipelinePath)}`;
}
