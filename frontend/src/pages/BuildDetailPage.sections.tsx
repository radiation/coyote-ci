import type { ReactNode } from "react";
import { Link } from "react-router-dom";
import { BuildArtifactsSection } from "../components/BuildArtifactsSection";
import { StatusBadge } from "../components/StatusBadge";
import { StepList } from "../components/StepList";
import { SummaryCard } from "../components/SummaryCard";
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
  shortSHA,
  triggerKind,
} from "./BuildDetailPage.helpers";

function summaryTone(
  status: Build["status"],
): "warning" | "success" | "danger" | "info" {
  if (status === "success") {
    return "success";
  }
  if (status === "failed") {
    return "danger";
  }
  if (status === "canceled") {
    return "info";
  }
  if (status === "running") {
    return "warning";
  }
  return "info";
}

function metadataItem(label: string, value: ReactNode) {
  return { label, value };
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
  const stepCounts = buildStepCounts(steps);
  const duration = buildDuration(build, buildUpdatedAt);
  const lastUpdatedLabel =
    buildUpdatedAt > 0
      ? formatTime(new Date(buildUpdatedAt).toISOString())
      : "—";

  return (
    <section className="panel dashboard-panel build-summary-panel">
      <div className="build-summary-header">
        <div>
          <div className="build-summary-title-row">
            <h3>{buildLabel(build)}</h3>
            <StatusBadge status={build.status} />
            <span className={`trigger-badge trigger-${triggerKind(build)}`}>
              {triggerKind(build)}
            </span>
          </div>
          <p className="subtle-text build-summary-subtitle">
            Build ID {build.id}
          </p>
          {build.rerun_of_build_id ? (
            <p className="subtle-text build-summary-subtitle">
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
            </p>
          ) : null}
        </div>
        <div className="build-summary-side subtle-text">
          Last updated {lastUpdatedLabel}
        </div>
      </div>

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
          <strong>Duration</strong>
          <span>{duration}</span>
        </div>
        <div>
          <strong>Current step</strong>
          <span>
            {stepsLoading
              ? "Loading…"
              : stepCounts.total > 0
                ? `${build.current_step_index} of ${stepCounts.total}`
                : build.current_step_index}
          </span>
        </div>
        <div>
          <strong>Priority</strong>
          <span>{build.priority}</span>
        </div>
        {build.trigger_ref ? (
          <div>
            <strong>Ref</strong>
            <span>{build.trigger_ref}</span>
          </div>
        ) : null}
      </div>
    </section>
  );
}

export function BuildStepSummaryGrid({
  build,
  steps,
  stepsLoading,
}: {
  build: Build;
  steps: BuildStep[] | undefined;
  stepsLoading: boolean;
}) {
  const stepCounts = buildStepCounts(steps);

  return (
    <section
      className="build-step-summary-grid"
      aria-label="Execution summary counts"
    >
      <SummaryCard
        title="Build"
        value={<StatusBadge status={build.status} />}
        tone={summaryTone(build.status)}
        description={build.error_message ?? undefined}
      />
      <SummaryCard
        title="Succeeded"
        value={stepsLoading ? "Loading…" : String(stepCounts.success)}
        tone={stepCounts.success > 0 ? "success" : "default"}
      />
      <SummaryCard
        title="Failed"
        value={stepsLoading ? "Loading…" : String(stepCounts.failed)}
        tone={stepCounts.failed > 0 ? "danger" : "default"}
      />
      <SummaryCard
        title="Canceled"
        value={stepsLoading ? "Loading…" : String(stepCounts.canceled)}
        tone={stepCounts.canceled > 0 ? "info" : "default"}
      />
      <SummaryCard
        title="Running"
        value={stepsLoading ? "Loading…" : String(stepCounts.running)}
        tone={stepCounts.running > 0 ? "warning" : "default"}
      />
      <SummaryCard
        title="Pending"
        value={stepsLoading ? "Loading…" : String(stepCounts.pending)}
        tone={stepCounts.pending > 0 ? "info" : "default"}
      />
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
          <p className="subtle-text">
            What ran, what failed, and where execution stopped.
          </p>
        </div>
      </div>

      {stepsLoading ? <p>Loading execution summary…</p> : null}
      {stepsError ? (
        <p className="error-text">Failed to load steps: {String(stepsError)}</p>
      ) : null}
      {!stepsLoading && !stepsError && (steps ?? []).length === 0 ? (
        <p className="subtle-text">No steps recorded for this build.</p>
      ) : null}
      {!stepsLoading && !stepsError && (steps ?? []).length > 0 ? (
        <div className="build-callout-list">
          {failedStep ? (
            <article className="build-callout build-callout-failed">
              <strong>Failed at step {failedStep.step_index}</strong>
              <span>{failedStep.name}</span>
              {failedStep.exit_code !== null ? (
                <span className="subtle-text">
                  Exit code {failedStep.exit_code}
                </span>
              ) : null}
              {failedStep.error_message ? (
                <p className="error-text">{failedStep.error_message}</p>
              ) : null}
            </article>
          ) : null}
          {!failedStep && runningStep ? (
            <article className="build-callout build-callout-running">
              <strong>Currently running</strong>
              <span>
                Step {runningStep.step_index} · {runningStep.name}
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
}: {
  build: Build;
  steps: BuildStep[] | undefined;
  stepsLoading: boolean;
  stepsError: unknown;
}) {
  return (
    <section className="detail-panel">
      <div className="dashboard-panel-header">
        <div>
          <h3>Execution timeline</h3>
          <p className="subtle-text">
            Open a step to inspect logs without flooding the page.
          </p>
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
          activeStepIndex={build.current_step_index}
        />
      ) : null}
    </section>
  );
}

export function LogsPanel({
  steps,
  stepsLoading,
  stepsError,
}: {
  steps: BuildStep[] | undefined;
  stepsLoading: boolean;
  stepsError: unknown;
}) {
  return (
    <section className="detail-panel">
      <div className="dashboard-panel-header">
        <div>
          <h3>Logs</h3>
          <p className="subtle-text">
            Jump to a step, then open its logs inline.
          </p>
        </div>
      </div>
      {stepsLoading ? <p>Loading log entry points…</p> : null}
      {stepsError ? (
        <p className="error-text">Step data is unavailable.</p>
      ) : null}
      {!stepsLoading && !stepsError && (steps ?? []).length === 0 ? (
        <p className="subtle-text">No step logs available yet.</p>
      ) : null}
      {!stepsLoading && !stepsError && (steps ?? []).length > 0 ? (
        <div className="build-log-link-list">
          {(steps ?? []).map((step) => (
            <a key={step.id} href={`#step-${step.step_index}`}>
              Step {step.step_index} · {step.name}
            </a>
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
  return (
    <section className="detail-panel">
      <div className="dashboard-panel-header">
        <div>
          <h3>Artifacts</h3>
          <p className="subtle-text">
            Build outputs grouped by build-level and step-level scope.
          </p>
        </div>
      </div>
      <BuildArtifactsSection
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
  const provenanceItems = [
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
      ? metadataItem("Pipeline path", build.pipeline_path)
      : null,
    build.scm_provider
      ? metadataItem("SCM provider", build.scm_provider)
      : null,
    build.event_type ? metadataItem("Event type", build.event_type) : null,
    build.trigger_ref ? metadataItem("Ref", build.trigger_ref) : null,
    build.ref_type ? metadataItem("Ref type", build.ref_type) : null,
    build.actor ? metadataItem("Actor", build.actor) : null,
    build.trigger_commit_sha &&
    build.source_commit_sha &&
    build.trigger_commit_sha !== build.source_commit_sha
      ? metadataItem("Trigger commit", shortSHA(build.trigger_commit_sha))
      : null,
    build.trigger_commit_sha &&
    build.source_commit_sha &&
    build.trigger_commit_sha !== build.source_commit_sha
      ? metadataItem("Source commit", shortSHA(build.source_commit_sha))
      : build.source_commit_sha || build.trigger_commit_sha
        ? metadataItem(
            "Commit",
            shortSHA(build.source_commit_sha ?? build.trigger_commit_sha),
          )
        : null,
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

  return (
    <section className="detail-panel">
      <div className="dashboard-panel-header">
        <div>
          <h3>Provenance</h3>
          <p className="subtle-text">
            Source, trigger, repository, and image metadata for this build.
          </p>
        </div>
      </div>

      {provenanceItems.length === 0 &&
      !build.image?.managed_image_version_id ? (
        <p className="subtle-text">
          No source metadata available for this build.
        </p>
      ) : (
        <div className="detail-grid build-provenance-grid">
          {provenanceItems.map((item) => (
            <div key={item.label}>
              <strong>{item.label}</strong>
              <span>{item.value}</span>
            </div>
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
    <>
      <Link className="secondary-button" to="/builds">
        Back to builds
      </Link>
      <Link className="secondary-button" to={`/projects/${build.project_id}`}>
        View project
      </Link>
      {build.job_id ? (
        <Link className="secondary-button" to={`/jobs/${build.job_id}`}>
          View job
        </Link>
      ) : null}
      {isRerunnableBuild(build.status) ? (
        <button
          className="secondary-button"
          type="button"
          onClick={onRerun}
          disabled={rerunPending}
        >
          {rerunPending ? "Rerunning…" : "Rerun"}
        </button>
      ) : null}
      {isCancelableBuild(build.status) ? (
        <button
          className="secondary-button danger-button"
          type="button"
          onClick={onCancel}
          disabled={cancelPending}
        >
          {cancelPending ? "Canceling…" : "Cancel"}
        </button>
      ) : null}
    </>
  );
}
