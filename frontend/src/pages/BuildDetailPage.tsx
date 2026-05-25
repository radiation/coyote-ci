import type { ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "react-router-dom";
import {
  createJobVersionTags,
  getBuild,
  getBuildArtifacts,
  getBuildSteps,
} from "../api";
import { BuildArtifactsSection } from "../components/BuildArtifactsSection";
import { PageHeader } from "../components/PageHeader";
import { StatusBadge } from "../components/StatusBadge";
import { StepList } from "../components/StepList";
import { SummaryCard } from "../components/SummaryCard";
import { VersionTagEditor } from "../components/VersionTagEditor";
import type { Build, BuildStep } from "../types";
import {
  FAST_POLL_INTERVAL,
  SLOW_POLL_INTERVAL,
  isActiveBuild,
} from "../utils/build";
import { formatCompactTime, formatDuration, formatTime } from "../utils/time";

function shortSHA(value: string | null | undefined): string {
  const trimmed = (value ?? "").trim();
  if (!trimmed) return "—";
  return trimmed.slice(0, 7);
}

function triggerKind(build: Build): string {
  return (build.trigger_kind ?? "manual").trim() || "manual";
}

function compactTriggerMetadata(build: Build): string {
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

function buildLabel(build: Build): string {
  return build.build_number
    ? `Build #${build.build_number}`
    : `Build ${build.id.slice(0, 8)}`;
}

function projectLabel(build: Build): string {
  return (
    build.project_name?.trim() || build.project_slug?.trim() || build.project_id
  );
}

function jobLabel(build: Build): string {
  const name = build.job_name?.trim();
  if (name) {
    return name;
  }

  const jobID = build.job_id?.trim();
  return jobID ? `Job ${jobID.slice(0, 8)}` : "Manual";
}

function buildDuration(build: Build, buildUpdatedAt: number): string {
  const endISO =
    build.finished_at ??
    (isActiveBuild(build.status) && buildUpdatedAt > 0
      ? new Date(buildUpdatedAt).toISOString()
      : null);
  return formatDuration(build.started_at, endISO);
}

function buildStepCounts(steps: BuildStep[] | undefined) {
  const counts = {
    total: 0,
    success: 0,
    failed: 0,
    running: 0,
    pending: 0,
  };

  for (const step of steps ?? []) {
    counts.total += 1;
    counts[step.status] += 1;
  }

  return counts;
}

function summaryTone(
  status: Build["status"],
): "warning" | "success" | "danger" | "info" {
  if (status === "success") {
    return "success";
  }
  if (status === "failed") {
    return "danger";
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

export function BuildDetailPage() {
  const { id } = useParams<{ id: string }>();
  const queryClient = useQueryClient();

  const {
    data: build,
    isLoading: buildLoading,
    error: buildError,
    dataUpdatedAt: buildUpdatedAt,
  } = useQuery({
    queryKey: ["build", id],
    queryFn: () => getBuild(id!),
    enabled: !!id,
    refetchInterval: (query) => {
      const nextBuild = query.state.data as Build | undefined;
      return isActiveBuild(nextBuild?.status)
        ? FAST_POLL_INTERVAL
        : SLOW_POLL_INTERVAL;
    },
  });

  const {
    data: steps,
    isLoading: stepsLoading,
    error: stepsError,
  } = useQuery({
    queryKey: ["buildSteps", id],
    queryFn: () => getBuildSteps(id!),
    enabled: !!id,
    refetchInterval: isActiveBuild(build?.status)
      ? FAST_POLL_INTERVAL
      : SLOW_POLL_INTERVAL,
  });

  const {
    data: artifacts,
    isLoading: artifactsLoading,
    error: artifactsError,
  } = useQuery({
    queryKey: ["buildArtifacts", id],
    queryFn: () => getBuildArtifacts(id!),
    enabled: !!id,
    refetchInterval: isActiveBuild(build?.status)
      ? FAST_POLL_INTERVAL
      : SLOW_POLL_INTERVAL,
  });

  const createVersionTagMutation = useMutation({
    mutationFn: ({
      jobID,
      version,
      artifactIDs,
      managedImageVersionIDs,
    }: {
      jobID: string;
      version: string;
      artifactIDs?: string[];
      managedImageVersionIDs?: string[];
    }) =>
      createJobVersionTags(jobID, {
        version,
        artifact_ids: artifactIDs,
        managed_image_version_ids: managedImageVersionIDs,
      }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["build", id] });
      await queryClient.invalidateQueries({ queryKey: ["buildArtifacts", id] });
    },
  });

  if (buildLoading) return <p>Loading build…</p>;
  if (buildError)
    return (
      <p className="error-text">Failed to load build: {String(buildError)}</p>
    );
  if (!build) return <p className="error-text">Build not found.</p>;

  const currentBuild = build;
  const stepCounts = buildStepCounts(steps);
  const failedStep = (steps ?? []).find((step) => step.status === "failed");
  const runningStep =
    (steps ?? []).find((step) => step.status === "running") ??
    (isActiveBuild(build.status)
      ? (steps ?? []).find(
          (step) => step.step_index === build.current_step_index,
        )
      : undefined);
  const duration = buildDuration(build, buildUpdatedAt);
  const lastUpdatedLabel =
    buildUpdatedAt > 0
      ? formatTime(new Date(buildUpdatedAt).toISOString())
      : "—";
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

  async function assignArtifactVersion(artifactID: string, version: string) {
    if (!currentBuild.job_id) {
      throw new Error("Build is not associated with a job.");
    }
    await createVersionTagMutation.mutateAsync({
      jobID: currentBuild.job_id,
      version,
      artifactIDs: [artifactID],
    });
  }

  async function assignManagedImageVersion(version: string) {
    if (!currentBuild.job_id || !currentBuild.image?.managed_image_version_id) {
      throw new Error("Build has no managed image version to tag.");
    }
    await createVersionTagMutation.mutateAsync({
      jobID: currentBuild.job_id,
      version,
      managedImageVersionIDs: [currentBuild.image.managed_image_version_id],
    });
  }

  return (
    <div className="page-content page-build-detail">
      <PageHeader
        eyebrow={
          <>
            <Link to={`/projects/${build.project_id}`}>
              {projectLabel(build)}
            </Link>
            {build.job_id ? (
              <>
                {" · "}
                <Link to={`/jobs/${build.job_id}`}>{jobLabel(build)}</Link>
              </>
            ) : null}
          </>
        }
        title={buildLabel(build)}
        description={
          compactTriggerMetadata(build) ||
          "Execution details, logs, artifacts, and provenance."
        }
        actions={
          <>
            <Link className="secondary-button" to="/builds">
              Back to builds
            </Link>
            <Link
              className="secondary-button"
              to={`/projects/${build.project_id}`}
            >
              View project
            </Link>
            {build.job_id ? (
              <Link className="secondary-button" to={`/jobs/${build.job_id}`}>
                View job
              </Link>
            ) : null}
          </>
        }
      />

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
          <p className="error-text">
            Failed to load steps: {String(stepsError)}
          </p>
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
          <p className="error-text">
            Failed to load steps: {String(stepsError)}
          </p>
        ) : null}
        {steps ? (
          <StepList
            buildID={build.id}
            steps={steps}
            activeStepIndex={build.current_step_index}
          />
        ) : null}
      </section>

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
          onAssignVersion={build.job_id ? assignArtifactVersion : undefined}
        />
      </section>

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
              onAssign={build.job_id ? assignManagedImageVersion : undefined}
            />
          </div>
        ) : null}
      </section>
    </div>
  );
}
