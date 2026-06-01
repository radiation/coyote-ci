import type { Build, BuildStep } from "../types";
import { isActiveBuild } from "../utils/build";
import {
  buildGitHubCommitURL,
  buildGitHubPipelinePathURL,
  buildGitHubRefURL,
  buildPrimaryCommitValue,
  buildSourceRefValue,
  shortSHA,
} from "../utils/provenance";
import { formatDuration } from "../utils/time";

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

export {
  buildGitHubCommitURL,
  buildGitHubPipelinePathURL,
  buildGitHubRefURL,
  buildPrimaryCommitValue,
  buildSourceRefValue,
  shortSHA,
};
