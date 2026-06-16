import { Fragment, type ReactNode } from "react";
import { Link } from "react-router-dom";
import { BuildArtifactsSection } from "../components/BuildArtifactsSection";
import { StatusBadge } from "../components/StatusBadge";
import { StepList } from "../components/StepList";
import { VersionTagEditor } from "../components/VersionTagEditor";
import type { Build, BuildArtifact, BuildStep } from "../types";
import {
  isActiveBuild,
  isCancelableBuild,
  isRerunnableBuild,
} from "../utils/build";
import { formatCompactTime, formatTime } from "../utils/time";
import {
  buildDuration,
  buildLabel,
  buildStepCounts,
  operationalBuildTitle,
  triggerKind,
} from "./BuildDetailPage.helpers";
import {
  buildGitHubCommitURL,
  buildGitHubPipelinePathURL,
  buildGitHubRefURL,
  buildPrimaryCommitValue,
  buildSourceRefValue,
  shortSHA,
} from "../utils/provenance";

function metadataItem(label: string, value: ReactNode) {
  return { label, value };
}

function nonEmptyValue(value: string | null | undefined): string | null {
  const trimmed = value?.trim();
  return trimmed ? trimmed : null;
}

function normalizeSnippet(value: string): string {
  return value.replace(/\s+/g, " ").trim();
}

function failureSnippet(step: BuildStep): string | null {
  const sources = [step.stderr, step.stdout];

  for (const source of sources) {
    const content = nonEmptyValue(source);
    if (!content) {
      continue;
    }

    const lines = content
      .split(/\r?\n/)
      .map((line) => normalizeSnippet(line))
      .filter(Boolean);
    const lastLine = lines.at(-1);

    if (!lastLine) {
      continue;
    }

    return lastLine.length > 180 ? `${lastLine.slice(0, 177)}...` : lastLine;
  }

  return null;
}

function textValue(value: string | null | undefined): string | null {
  const trimmed = value?.trim();
  return trimmed ? trimmed : null;
}

function formatIdentityValue(
  name: string | null | undefined,
  email: string | null | undefined,
): string | null {
  const normalizedName = textValue(name);
  const normalizedEmail = textValue(email);

  if (normalizedName && normalizedEmail) {
    return `${normalizedName} <${normalizedEmail}>`;
  }
  return normalizedName ?? normalizedEmail;
}

function repositoryDisplayLabel(build: Build): string {
  return `${build.repository_owner ?? ""}${build.repository_owner && build.repository_name ? "/" : ""}${build.repository_name ?? build.repository_url ?? ""}`;
}

function isSafeExternalURL(value: string | null | undefined): boolean {
  const trimmed = value?.trim();
  if (!trimmed) {
    return false;
  }

  try {
    const parsed = new URL(trimmed);
    return parsed.protocol === "http:" || parsed.protocol === "https:";
  } catch {
    return false;
  }
}

function stepSummaryText(
  stepCounts: ReturnType<typeof buildStepCounts>,
): string {
  const entries = [
    stepCounts.success > 0 ? `${stepCounts.success} succeeded` : null,
    stepCounts.failed > 0 ? `${stepCounts.failed} failed` : null,
    stepCounts.canceled > 0 ? `${stepCounts.canceled} canceled` : null,
    stepCounts.running > 0 ? `${stepCounts.running} running` : null,
    stepCounts.pending > 0 ? `${stepCounts.pending} pending` : null,
  ].filter((value): value is string => Boolean(value));

  return entries.length > 0
    ? `Steps: ${entries.join(" · ")}`
    : "Steps: none recorded yet";
}

function displayStepNumber(stepIndex: number): number {
  return stepIndex + 1;
}

function stepLogLinkClassName(step: BuildStep): string {
  if (step.status === "failed") {
    return "build-log-link is-failed";
  }

  if (step.status === "running") {
    return "build-log-link is-running";
  }

  if (step.status === "pending") {
    return "build-log-link is-pending";
  }

  return "build-log-link";
}

type LogStepGroup = {
  key: string;
  label: string;
  steps: BuildStep[];
};

function normalizeLogGroupLabel(groupName: string | null | undefined): string {
  const trimmed = groupName?.trim();
  return trimmed ? trimmed : "Ungrouped";
}

function groupLogSteps(steps: BuildStep[]): LogStepGroup[] {
  const groups = new Map<string, LogStepGroup>();

  for (const step of steps) {
    const label = normalizeLogGroupLabel(step.group_name);
    const existing = groups.get(label);

    if (existing) {
      existing.steps.push(step);
      continue;
    }

    groups.set(label, {
      key: label,
      label,
      steps: [step],
    });
  }

  return Array.from(groups.values());
}

function logGroupSummaryText(steps: BuildStep[]): string {
  const counts = {
    success: 0,
    failed: 0,
    running: 0,
    pending: 0,
    canceled: 0,
  };

  for (const step of steps) {
    counts[step.status] += 1;
  }

  const stepSummary = `${steps.length} step${steps.length === 1 ? "" : "s"}`;

  if (counts.failed > 0) {
    return `${stepSummary} · ${counts.failed} failed`;
  }

  if (counts.running > 0) {
    return `${stepSummary} · ${counts.running} running`;
  }

  if (counts.pending === steps.length) {
    return `${stepSummary} · pending`;
  }

  if (counts.pending > 0) {
    return `${stepSummary} · ${counts.pending} pending`;
  }

  if (counts.canceled === steps.length) {
    return `${stepSummary} · canceled`;
  }

  if (counts.canceled > 0) {
    return `${stepSummary} · ${counts.canceled} canceled`;
  }

  if (counts.success > 0) {
    return `${stepSummary} · ${counts.success} succeeded`;
  }

  return stepSummary;
}

function currentStepSummaryLabel(
  currentStepIndex: number,
  totalSteps: number,
): string {
  if (totalSteps <= 0) {
    return `Step ${displayStepNumber(currentStepIndex)}`;
  }

  const clampedStepIndex = Math.min(
    Math.max(currentStepIndex, 0),
    totalSteps - 1,
  );
  return `Step ${displayStepNumber(clampedStepIndex)} of ${totalSteps}`;
}

export function BuildSummaryPanel({
  build,
  rerunSourceBuild,
  steps,
  stepsLoading,
  buildUpdatedAt,
}: {
  build: Build;
  rerunSourceBuild?: Build;
  steps: BuildStep[] | undefined;
  stepsLoading: boolean;
  buildUpdatedAt: number;
}) {
  const provenanceAnchorID = "build-provenance";
  const stepCounts = buildStepCounts(steps);
  const failedStep = (steps ?? []).find((step) => step.status === "failed");
  const duration = buildDuration(build, buildUpdatedAt);
  const lastUpdatedLabel =
    buildUpdatedAt > 0
      ? formatTime(new Date(buildUpdatedAt).toISOString())
      : "—";
  const currentStepLabel = stepsLoading
    ? "Loading…"
    : currentStepSummaryLabel(build.current_step_index, stepCounts.total);
  const contextItems = [
    [
      textValue(build.trigger_kind),
      textValue(build.trigger_type),
      textValue(build.event_type),
    ].filter(
      (value, index, values) =>
        Boolean(value) && values.indexOf(value) === index,
    ).length > 0
      ? metadataItem(
          "Trigger",
          [
            textValue(build.trigger_kind),
            textValue(build.trigger_type),
            textValue(build.event_type),
          ]
            .filter((value, index, values): value is string => {
              return Boolean(value) && values.indexOf(value) === index;
            })
            .join(" / "),
        )
      : null,
    textValue(build.source_ref) || textValue(build.trigger_ref)
      ? metadataItem(
          "Ref",
          textValue(build.source_ref) ?? textValue(build.trigger_ref) ?? "—",
        )
      : null,
    textValue(build.source_sha) ||
    textValue(build.source_commit_sha) ||
    textValue(build.trigger_commit_sha)
      ? metadataItem(
          "Commit",
          shortSHA(
            textValue(build.source_sha) ??
              textValue(build.source_commit_sha) ??
              textValue(build.trigger_commit_sha),
          ),
        )
      : null,
    textValue(build.pipeline_name) || textValue(build.pipeline_path)
      ? metadataItem(
          "Pipeline",
          textValue(build.pipeline_name) ??
            textValue(build.pipeline_path) ??
            "—",
        )
      : null,
    metadataItem("Priority", String(build.priority)),
  ].filter((item): item is { label: string; value: ReactNode } =>
    Boolean(item),
  );

  return (
    <section className="panel dashboard-panel build-summary-panel">
      <div className="build-summary-hero">
        <div className="build-summary-primary">
          <p className="build-summary-kicker">Operational overview</p>
          <div className="build-summary-title-row">
            <h3>{operationalBuildTitle(build)}</h3>
            <StatusBadge status={build.status} />
            <span className={`trigger-badge trigger-${triggerKind(build)}`}>
              {triggerKind(build)}
            </span>
          </div>
          <p className="subtle-text build-summary-subtitle">
            {buildLabel(build)} · Build ID {build.id} · Attempt{" "}
            {build.attempt_number}
          </p>
          {build.error_message && !(build.status === "failed" && failedStep) ? (
            <p className="error-text build-summary-error">
              {build.error_message}
            </p>
          ) : null}
        </div>
        <div className="build-summary-side">
          <div className="build-summary-side-card">
            <strong>Duration</strong>
            <span>{duration}</span>
          </div>
          <div className="build-summary-side-card">
            <strong>Last updated</strong>
            <span>{lastUpdatedLabel}</span>
          </div>
        </div>
      </div>

      {build.rerun_of_build_id ? (
        <div className="build-lineage-banner">
          <div>
            <strong>
              Rerun of{" "}
              <Link to={`/builds/${build.rerun_of_build_id}`}>
                {buildLabel(
                  rerunSourceBuild ?? {
                    ...build,
                    id: build.rerun_of_build_id,
                    build_number: undefined,
                  },
                )}
              </Link>
            </strong>
            <p className="subtle-text">
              Compare this attempt against the original run from the same job.
              {build.rerun_from_step_index !== null &&
              build.rerun_from_step_index !== undefined
                ? ` Restarted from step ${displayStepNumber(build.rerun_from_step_index)}.`
                : ""}
            </p>
          </div>
        </div>
      ) : null}

      <div className="build-summary-facts">
        <div>
          <strong>Created</strong>
          <span>{formatCompactTime(build.created_at)}</span>
        </div>
        <div>
          <strong>Queued</strong>
          <span>{formatCompactTime(build.queued_at)}</span>
        </div>
        <div>
          <strong>Started</strong>
          <span>{formatCompactTime(build.started_at)}</span>
        </div>
        <div>
          <strong>Finished</strong>
          <span>{formatCompactTime(build.finished_at)}</span>
        </div>
        <div>
          <strong>Current step</strong>
          <span>{currentStepLabel}</span>
        </div>
      </div>

      {contextItems.length > 0 ? (
        <a
          className="build-summary-context-strip subtle-text"
          href={`#${provenanceAnchorID}`}
          aria-label="View full provenance details"
        >
          {contextItems.map((item, index) => (
            <Fragment key={item.label}>
              {index > 0 ? (
                <span
                  className="build-summary-context-separator"
                  aria-hidden="true"
                >
                  ·
                </span>
              ) : null}
              <span className="build-summary-context-item">
                <span className="build-summary-context-label">
                  {item.label}
                </span>
                <span className="build-summary-context-value">
                  {item.value}
                </span>
              </span>
            </Fragment>
          ))}
          <span className="build-summary-context-separator" aria-hidden="true">
            ·
          </span>
          <span className="build-summary-context-link" aria-hidden="true">
            View details ↓
          </span>
        </a>
      ) : null}
    </section>
  );
}

export function ExecutionSummaryPanel({
  build,
  steps,
  stepsLoading,
  stepsError,
}: {
  build: Build;
  steps: BuildStep[] | undefined;
  stepsLoading: boolean;
  stepsError: unknown;
}) {
  const stepCounts = buildStepCounts(steps);
  const failedStep = (steps ?? []).find((step) => step.status === "failed");
  const failedStepPosition =
    failedStep && stepCounts.total > 0
      ? `Step ${failedStep.step_index + 1} of ${stepCounts.total}`
      : failedStep
        ? `Step ${failedStep.step_index + 1}`
        : null;
  const failedStepMessage = failedStep
    ? (nonEmptyValue(failedStep.error_message) ??
      nonEmptyValue(build.error_message))
    : null;
  const failedStepSnippet = failedStep ? failureSnippet(failedStep) : null;
  const showFailedStepSnippet =
    failedStepSnippet && failedStepSnippet !== failedStepMessage;
  const runningStep =
    (steps ?? []).find((step) => step.status === "running") ??
    (isActiveBuild(build.status)
      ? (steps ?? []).find(
          (step) => step.step_index === build.current_step_index,
        )
      : undefined);

  return (
    <section className="detail-panel">
      <div className="dashboard-panel-header">
        <div>
          <h3>Execution summary</h3>
          {!stepsLoading && !stepsError && (steps ?? []).length > 0 ? (
            <p className="build-steps-summary subtle-text">
              {stepSummaryText(stepCounts)}
            </p>
          ) : null}
        </div>
      </div>

      {stepsLoading ? <p>Loading execution summary…</p> : null}
      {stepsError ? (
        <p className="error-text">Failed to load steps: {String(stepsError)}</p>
      ) : null}
      {!stepsLoading && !stepsError && (steps ?? []).length === 0 ? (
        <p className="subtle-text">
          No steps were recorded for this build. If it just started, wait for
          the runner to report execution; otherwise rerun the job to capture a
          fresh attempt.
        </p>
      ) : null}
      {!stepsLoading && !stepsError && (steps ?? []).length > 0 ? (
        <div className="build-callout-list">
          {failedStep ? (
            <article className="build-callout build-callout-failed">
              <strong>Failed step</strong>
              <span className="build-callout-primary">{failedStep.name}</span>
              <div className="build-callout-meta-grid">
                {failedStepPosition ? (
                  <span className="subtle-text">{failedStepPosition}</span>
                ) : null}
                {failedStep.exit_code !== null ? (
                  <span className="subtle-text">
                    Exit code {failedStep.exit_code}
                  </span>
                ) : null}
              </div>
              {failedStepMessage ? (
                <p className="error-text">{failedStepMessage}</p>
              ) : null}
              {showFailedStepSnippet ? (
                <p className="build-callout-snippet">
                  <span className="build-callout-snippet-label">
                    Last error output
                  </span>
                  <span>{failedStepSnippet}</span>
                </p>
              ) : null}
              <p className="subtle-text">
                Build stopped after this step failed.
              </p>
            </article>
          ) : null}
          {!failedStep && runningStep ? (
            <article className="build-callout build-callout-running">
              <strong>Currently running</strong>
              <span>
                Step {displayStepNumber(runningStep.step_index)} ·{" "}
                {runningStep.name}
              </span>
              {stepCounts.pending > 0 ? (
                <span className="subtle-text">
                  {stepCounts.pending} pending{" "}
                  {stepCounts.pending === 1 ? "step" : "steps"}
                </span>
              ) : null}
            </article>
          ) : null}
          {!failedStep && !runningStep && build.status === "success" ? (
            <article className="build-callout build-callout-success">
              <strong>Completed successfully</strong>
              <span>All recorded steps finished successfully.</span>
            </article>
          ) : null}
        </div>
      ) : null}
    </section>
  );
}

export function StepTimelinePanel({
  build,
  steps,
  stepsLoading,
  stepsError,
  openStepIndex,
  onOpenStepChange,
}: {
  build: Build;
  steps: BuildStep[] | undefined;
  stepsLoading: boolean;
  stepsError: unknown;
  openStepIndex: number | null;
  onOpenStepChange: (stepIndex: number | null) => void;
}) {
  return (
    <section className="detail-panel">
      <div className="dashboard-panel-header">
        <div>
          <h3>Execution timeline</h3>
        </div>
      </div>
      {stepsLoading ? <p>Loading steps…</p> : null}
      {stepsError ? (
        <p className="error-text">Failed to load steps: {String(stepsError)}</p>
      ) : null}
      {steps ? (
        <StepList
          buildID={build.id}
          steps={steps}
          openStepIndex={openStepIndex}
          onOpenStepChange={onOpenStepChange}
        />
      ) : null}
    </section>
  );
}

export function LogsPanel({
  steps,
  stepsLoading,
  stepsError,
  onOpenStep,
}: {
  steps: BuildStep[] | undefined;
  stepsLoading: boolean;
  stepsError: unknown;
  onOpenStep: (stepIndex: number) => void;
}) {
  const stepCount = (steps ?? []).length;
  const stepGroups = groupLogSteps(steps ?? []);

  return (
    <section className="detail-panel">
      <div className="dashboard-panel-header">
        <div>
          <h3>Logs</h3>
          {stepCount > 0 ? (
            <p className="subtle-text">
              {stepCount} step{stepCount === 1 ? "" : "s"}
            </p>
          ) : null}
        </div>
      </div>
      {stepsLoading ? <p>Loading log entry points…</p> : null}
      {stepsError ? (
        <p className="error-text">
          Step data is unavailable. Retry the page to fetch log entry points
          again.
        </p>
      ) : null}
      {!stepsLoading && !stepsError && (steps ?? []).length === 0 ? (
        <p className="subtle-text">
          No step logs are available yet. When execution starts, open a step in
          the timeline to inspect stdout and stderr inline.
        </p>
      ) : null}
      {!stepsLoading && !stepsError && (steps ?? []).length > 0 ? (
        <div className="build-log-group-list">
          {stepGroups.map((group) => (
            <section
              key={group.key}
              className="build-log-group"
              aria-label={`Log group ${group.label}`}
            >
              <div className="build-log-group-heading">
                <strong>{group.label}</strong>
                <span className="build-log-group-meta subtle-text">
                  {logGroupSummaryText(group.steps)}
                </span>
              </div>
              <div className="build-log-link-list">
                {group.steps.map((step) => (
                  <a
                    key={step.id}
                    className={stepLogLinkClassName(step)}
                    href={`#step-${step.step_index}`}
                    onClick={() => onOpenStep(step.step_index)}
                    aria-label={`Open logs for ${step.name}`}
                    title={`Open logs for ${step.name}`}
                  >
                    <div className="build-log-link-header">
                      <strong>{step.name}</strong>
                      <StatusBadge status={step.status} />
                    </div>
                  </a>
                ))}
              </div>
            </section>
          ))}
        </div>
      ) : null}
    </section>
  );
}

export function ArtifactsPanel({
  build,
  steps,
  artifacts,
  artifactsLoading,
  artifactsError,
  onAssignVersion,
}: {
  build: Build;
  steps: BuildStep[] | undefined;
  artifacts: BuildArtifact[] | undefined;
  artifactsLoading: boolean;
  artifactsError: unknown;
  onAssignVersion:
    | ((artifactID: string, version: string) => Promise<void>)
    | undefined;
}) {
  const artifactCount = (artifacts ?? []).length;
  const buildLevelCount = (artifacts ?? []).filter(
    (artifact) => !artifact.step_id,
  ).length;
  const stepLevelCount = artifactCount - buildLevelCount;

  return (
    <section className="detail-panel">
      <div className="dashboard-panel-header">
        <div>
          <h3>Artifacts</h3>
          <p className="subtle-text">
            {artifactsLoading
              ? "Build outputs grouped by build-level and step-level scope."
              : artifactCount > 0
                ? `${artifactCount} artifact${artifactCount === 1 ? "" : "s"} collected. ${buildLevelCount} build-level, ${stepLevelCount} step-scoped.`
                : "Published files appear here after artifact-producing steps complete."}
          </p>
        </div>
      </div>
      <BuildArtifactsSection
        build={build}
        artifacts={artifacts ?? []}
        steps={steps}
        isLoading={artifactsLoading}
        error={artifactsError}
        onAssignVersion={build.job_id ? onAssignVersion : undefined}
      />
    </section>
  );
}

export function ProvenancePanel({
  build,
  onAssignManagedImageVersion,
}: {
  build: Build;
  onAssignManagedImageVersion: ((version: string) => Promise<void>) | undefined;
}) {
  const repositoryLabel = repositoryDisplayLabel(build);
  const sourceRef = buildSourceRefValue(build);
  const sourceRefHref = buildGitHubRefURL(build, sourceRef);
  const pipelinePathHref = buildGitHubPipelinePathURL(build);
  const primaryCommit = buildPrimaryCommitValue(build);
  const primaryCommitHref = buildGitHubCommitURL(build, primaryCommit);
  const sourceCommit =
    textValue(build.source_sha) ?? textValue(build.source_commit_sha);
  const sourceCommitHref = buildGitHubCommitURL(build, sourceCommit);
  const triggerCommit = textValue(build.trigger_commit_sha);
  const triggerCommitHref = buildGitHubCommitURL(build, triggerCommit);
  const authorValue = formatIdentityValue(
    build.source_author_name,
    build.source_author_email,
  );
  const committerValue = formatIdentityValue(
    build.source_committer_name,
    build.source_committer_email,
  );
  const sourceItems = [
    build.repository_url
      ? metadataItem(
          "Repository",
          isSafeExternalURL(build.repository_url) ? (
            <a href={build.repository_url}>{repositoryLabel}</a>
          ) : (
            repositoryLabel
          ),
        )
      : build.repository_owner || build.repository_name
        ? metadataItem("Repository", repositoryLabel)
        : null,
    build.pipeline_source
      ? metadataItem("Pipeline source", build.pipeline_source)
      : null,
    build.pipeline_path
      ? metadataItem(
          "Pipeline path",
          pipelinePathHref ? (
            <a href={pipelinePathHref}>{build.pipeline_path}</a>
          ) : (
            build.pipeline_path
          ),
        )
      : null,
    build.scm_provider
      ? metadataItem("SCM provider", build.scm_provider)
      : null,
    build.event_type ? metadataItem("Event type", build.event_type) : null,
    sourceRef
      ? metadataItem(
          "Ref",
          sourceRefHref ? <a href={sourceRefHref}>{sourceRef}</a> : sourceRef,
        )
      : null,
    build.ref_type ? metadataItem("Ref type", build.ref_type) : null,
    build.actor ? metadataItem("Actor", build.actor) : null,
    triggerCommit && sourceCommit && triggerCommit !== sourceCommit
      ? metadataItem(
          "Trigger commit",
          triggerCommitHref ? (
            <a href={triggerCommitHref}>{shortSHA(triggerCommit)}</a>
          ) : (
            shortSHA(triggerCommit)
          ),
        )
      : null,
    triggerCommit && sourceCommit && triggerCommit !== sourceCommit
      ? metadataItem(
          "Source commit",
          sourceCommitHref ? (
            <a href={sourceCommitHref}>{shortSHA(sourceCommit)}</a>
          ) : (
            shortSHA(sourceCommit)
          ),
        )
      : primaryCommit
        ? metadataItem(
            "Commit",
            primaryCommitHref ? (
              <a href={primaryCommitHref}>{shortSHA(primaryCommit)}</a>
            ) : (
              shortSHA(primaryCommit)
            ),
          )
        : null,
    authorValue ? metadataItem("Author", authorValue) : null,
    committerValue ? metadataItem("Committer", committerValue) : null,
  ].filter((item): item is { label: string; value: ReactNode } =>
    Boolean(item),
  );

  const runtimeImageItems = [
    build.image?.source_kind
      ? metadataItem("Image source", build.image.source_kind)
      : null,
    build.image?.requested_ref
      ? metadataItem("Requested image ref", build.image.requested_ref)
      : null,
    build.image?.resolved_ref
      ? metadataItem("Resolved image ref", build.image.resolved_ref)
      : null,
  ].filter((item): item is { label: string; value: ReactNode } =>
    Boolean(item),
  );

  const provenanceGroups = [
    sourceItems.length > 0 ? { title: "Source", items: sourceItems } : null,
    runtimeImageItems.length > 0
      ? { title: "Runtime image", items: runtimeImageItems }
      : null,
  ].filter(
    (
      group,
    ): group is {
      title: string;
      items: { label: string; value: ReactNode }[];
    } => Boolean(group),
  );

  return (
    <section className="detail-panel" id="build-provenance">
      <div className="dashboard-panel-header">
        <div>
          <h3>Provenance</h3>
          <p className="subtle-text">
            Source and runtime context for how this build was defined and run.
          </p>
        </div>
      </div>

      {provenanceGroups.length === 0 &&
      !build.image?.managed_image_version_id ? (
        <p className="subtle-text">
          No source metadata is available for this build. Manual or
          fixture-driven runs may omit repository and trigger context.
        </p>
      ) : (
        <div className="build-provenance-groups">
          {provenanceGroups.map((group) => (
            <section key={group.title} className="build-provenance-group">
              <div className="build-provenance-group-header">
                <h4>{group.title}</h4>
              </div>
              <div className="build-provenance-grid">
                {group.items.map((item) => (
                  <div key={item.label} className="build-provenance-item">
                    <span className="build-provenance-label">{item.label}</span>
                    <span className="build-provenance-value">{item.value}</span>
                  </div>
                ))}
              </div>
            </section>
          ))}
        </div>
      )}

      {build.image?.managed_image_version_id ? (
        <div className="build-provenance-tags">
          <div className="dashboard-panel-header">
            <div>
              <h4>Managed image version tags</h4>
              <p className="subtle-text">
                Existing version labels for the managed build image.
              </p>
            </div>
          </div>
          <VersionTagEditor
            tags={build.image.version_tags ?? []}
            emptyText="No version tags for this managed image version yet."
            inputLabel="managed-image-version-tag"
            onAssign={build.job_id ? onAssignManagedImageVersion : undefined}
          />
        </div>
      ) : null}
    </section>
  );
}

export function BuildDetailHeaderActions({
  build,
  cancelPending,
  rerunPending,
  onCancel,
  onRerun,
}: {
  build: Build;
  cancelPending: boolean;
  rerunPending: boolean;
  onCancel: () => void;
  onRerun: () => void;
}) {
  return (
    <div className="build-detail-header-actions">
      <Link className="build-detail-nav-action" to="/builds">
        Back to builds
      </Link>
      <Link
        className="build-detail-nav-action"
        to={`/projects/${build.project_id}`}
      >
        View project
      </Link>
      {build.job_id ? (
        <Link className="build-detail-nav-action" to={`/jobs/${build.job_id}`}>
          View job
        </Link>
      ) : null}
      {isRerunnableBuild(build.status) ? (
        <button
          className="build-detail-primary-action"
          type="button"
          onClick={onRerun}
          disabled={rerunPending}
        >
          {rerunPending ? "Rerunning…" : "Rerun"}
        </button>
      ) : null}
      {isCancelableBuild(build.status) ? (
        <button
          className="build-detail-nav-action danger-button"
          type="button"
          onClick={onCancel}
          disabled={cancelPending}
        >
          {cancelPending ? "Canceling…" : "Cancel"}
        </button>
      ) : null}
    </div>
  );
}
