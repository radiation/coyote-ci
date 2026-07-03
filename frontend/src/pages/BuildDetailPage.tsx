import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Link,
  useNavigate,
  useParams,
  useSearchParams,
} from "react-router-dom";
import {
  cancelBuild,
  createJobVersionTags,
  getBuild,
  getBuildArtifacts,
  getJob,
  getBuildSteps,
  rerunBuild,
} from "../api";
import { PageHeader } from "../components/PageHeader";
import type { Build } from "../types";
import {
  FAST_POLL_INTERVAL,
  SLOW_POLL_INTERVAL,
  isActiveBuild,
  isCancelableBuild,
  isRerunnableBuild,
} from "../utils/build";
import {
  ArtifactsPanel,
  BuildDetailHeaderActions,
  BuildSummaryPanel,
  ExecutionSummaryPanel,
  LogsPanel,
  ProvenancePanel,
  StepTimelinePanel,
} from "./BuildDetailPage.sections";
import {
  buildLabel,
  compactTriggerMetadata,
  operationalBuildTitle,
  jobLabel,
  projectLabel,
} from "./BuildDetailPage.helpers";

export function BuildDetailPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [searchParams, setSearchParams] = useSearchParams();
  const requestedOpenStepIndex = parseOpenStepIndex(searchParams.get("step"));

  function handleOpenStepChange(stepIndex: number | null) {
    const nextParams = new URLSearchParams(searchParams);
    if (stepIndex === null) {
      nextParams.delete("step");
    } else {
      nextParams.set("step", String(stepIndex));
    }
    setSearchParams(nextParams, { replace: true });
  }

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

  const { data: rerunSourceBuild } = useQuery({
    queryKey: ["build", build?.rerun_of_build_id],
    queryFn: () => getBuild(build!.rerun_of_build_id!),
    enabled: Boolean(build?.rerun_of_build_id),
  });

  const { data: job } = useQuery({
    queryKey: ["job", build?.job_id],
    queryFn: () => getJob(build!.job_id!),
    enabled: Boolean(build?.job_id && !build?.job_name?.trim()),
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

  const openStepIndex =
    requestedOpenStepIndex !== null &&
    steps?.some((step) => step.step_index === requestedOpenStepIndex)
      ? requestedOpenStepIndex
      : null;

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

  const cancelBuildMutation = useMutation({
    mutationFn: () => cancelBuild(id!),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["build", id] }),
        queryClient.invalidateQueries({ queryKey: ["buildSteps", id] }),
        queryClient.invalidateQueries({ queryKey: ["buildArtifacts", id] }),
        queryClient.invalidateQueries({ queryKey: ["queue"] }),
        queryClient.invalidateQueries({ queryKey: ["builds"] }),
      ]);
    },
  });

  const rerunBuildMutation = useMutation({
    mutationFn: () => rerunBuild(id!),
    onSuccess: async (newBuild) => {
      queryClient.setQueryData(["build", newBuild.id], newBuild);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["build", id] }),
        queryClient.invalidateQueries({ queryKey: ["queue"] }),
        queryClient.invalidateQueries({ queryKey: ["builds"] }),
      ]);
      navigate(`/builds/${newBuild.id}`);
    },
  });

  if (buildLoading) return <p>Loading build…</p>;
  if (buildError)
    return (
      <p className="error-text">Failed to load build: {String(buildError)}</p>
    );
  if (!build) return <p className="error-text">Build not found.</p>;

  const currentBuild =
    !build.job_name?.trim() && job?.name?.trim()
      ? { ...build, job_name: job.name }
      : build;

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

  async function requestCancelBuild() {
    if (!isCancelableBuild(currentBuild.status)) {
      return;
    }
    const confirmed = window.confirm(`Cancel ${buildLabel(currentBuild)}?`);
    if (!confirmed) {
      return;
    }
    await cancelBuildMutation.mutateAsync();
  }

  async function requestRerunBuild() {
    if (!isRerunnableBuild(currentBuild.status)) {
      return;
    }
    const confirmed = window.confirm(`Rerun ${buildLabel(currentBuild)}?`);
    if (!confirmed) {
      return;
    }
    await rerunBuildMutation.mutateAsync();
  }

  return (
    <div className="page-content page-build-detail">
      <PageHeader
        eyebrow={
          <>
            <Link to={`/projects/${currentBuild.project_id}`}>
              {projectLabel(currentBuild)}
            </Link>
            {currentBuild.job_id ? (
              <>
                {" · "}
                <Link to={`/jobs/${currentBuild.job_id}`}>
                  {jobLabel(currentBuild)}
                </Link>
              </>
            ) : null}
          </>
        }
        title={operationalBuildTitle(currentBuild)}
        description={
          compactTriggerMetadata(currentBuild) ||
          "Execution details, logs, artifacts, and provenance."
        }
        actions={
          <BuildDetailHeaderActions
            build={currentBuild}
            cancelPending={cancelBuildMutation.isPending}
            rerunPending={rerunBuildMutation.isPending}
            onCancel={() => void requestCancelBuild()}
            onRerun={() => void requestRerunBuild()}
          />
        }
      />

      {cancelBuildMutation.error ? (
        <p className="error-text">
          Failed to cancel build: {String(cancelBuildMutation.error)}
        </p>
      ) : null}
      {rerunBuildMutation.error ? (
        <p className="error-text">
          Failed to rerun build: {String(rerunBuildMutation.error)}
        </p>
      ) : null}

      <BuildSummaryPanel
        build={currentBuild}
        rerunSourceBuild={rerunSourceBuild}
        steps={steps}
        stepsLoading={stepsLoading}
        buildUpdatedAt={buildUpdatedAt}
      />
      <ExecutionSummaryPanel
        build={currentBuild}
        steps={steps}
        stepsLoading={stepsLoading}
        stepsError={stepsError}
      />
      <LogsPanel
        steps={steps}
        stepsLoading={stepsLoading}
        stepsError={stepsError}
        onOpenStep={handleOpenStepChange}
      />
      <StepTimelinePanel
        build={currentBuild}
        steps={steps}
        stepsLoading={stepsLoading}
        stepsError={stepsError}
        openStepIndex={openStepIndex}
        onOpenStepChange={handleOpenStepChange}
      />
      <ArtifactsPanel
        build={currentBuild}
        steps={steps}
        artifacts={artifacts}
        artifactsLoading={artifactsLoading}
        artifactsError={artifactsError}
        onAssignVersion={assignArtifactVersion}
      />
      <ProvenancePanel
        build={currentBuild}
        onAssignManagedImageVersion={assignManagedImageVersion}
      />
    </div>
  );
}

function parseOpenStepIndex(value: string | null): number | null {
  if (value === null) {
    return null;
  }
  if (!/^(0|[1-9]\d*)$/.test(value)) {
    return null;
  }
  const parsed = Number.parseInt(value, 10);
  if (!Number.isSafeInteger(parsed) || parsed < 0) {
    return null;
  }
  return parsed;
}
