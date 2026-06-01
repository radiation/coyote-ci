import type { Build } from "../types";

function textValue(value: string | null | undefined): string | null {
  const trimmed = value?.trim();
  return trimmed ? trimmed : null;
}

export function safeRepositoryURL(build: Build | undefined): URL | null {
  const candidate =
    textValue(build?.repository_url) ??
    textValue(build?.source?.repository_url);
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

export function githubRepositoryBaseURL(
  build: Build | undefined,
): string | null {
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
  if (!trimmed) {
    return "—";
  }
  return trimmed.slice(0, 7);
}

export function buildSourceRefValue(build: Build | undefined): string | null {
  if (!build) {
    return null;
  }

  return (
    textValue(build.source_ref) ??
    textValue(build.source?.ref) ??
    textValue(build.trigger_ref)
  );
}

export function buildPrimaryCommitValue(
  build: Build | undefined,
): string | null {
  if (!build) {
    return null;
  }

  return (
    textValue(build.source_sha) ??
    textValue(build.source_commit_sha) ??
    textValue(build.source?.source_commit_sha) ??
    textValue(build.trigger_commit_sha)
  );
}

export function buildGitHubCommitURL(
  build: Build | undefined,
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
  build: Build | undefined,
  ref: string | null | undefined,
): string | null {
  const baseURL = githubRepositoryBaseURL(build);
  const refValue = textValue(ref);
  if (!baseURL || !refValue) {
    return null;
  }

  return `${baseURL}/tree/${encodePathSegments(normalizeGitHubRef(refValue))}`;
}

export function buildGitHubPipelinePathURL(
  build: Build | undefined,
): string | null {
  const baseURL = githubRepositoryBaseURL(build);
  const pipelinePath = textValue(build?.pipeline_path);
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
