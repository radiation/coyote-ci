import { useQuery } from "@tanstack/react-query";
import {
  listBuilds,
  listBuildsByJob,
  listJobsByProject,
  listQueue,
} from "../api";
import { jobBuildsQueryOptions } from "../queries/jobBuilds";
import type { Build, QueueEntry } from "../types/build";
import {
  FAST_POLL_INTERVAL,
  SLOW_POLL_INTERVAL,
  isActiveBuild,
} from "../utils/build";
import {
  BuildActivityPanel,
  type BuildActivityItem,
} from "./BuildActivityList";

const DEFAULT_ACTIVITY_LIMIT = 6;

type BuildActivityScope =
  | { type: "global" }
  | { type: "project"; projectId: string }
  | { type: "job"; jobId: string };

function isQueueStatus(
  status: Build["status"],
): status is QueueEntry["status"] {
  return status === "queued" || status === "running";
}

function scopeQuerySuffix(scope: BuildActivityScope): string {
  if (scope.type === "global") {
    return "global";
  }
  if (scope.type === "project") {
    return `project:${scope.projectId}`;
  }
  return `job:${scope.jobId}`;
}

function contextModeForScope(scope: BuildActivityScope) {
  return scope.type;
}

function sortByNewest(builds: Build[]): Build[] {
  return [...builds].sort((left, right) => {
    const leftTime = Date.parse(
      left.finished_at ?? left.started_at ?? left.queued_at ?? left.created_at,
    );
    const rightTime = Date.parse(
      right.finished_at ??
        right.started_at ??
        right.queued_at ??
        right.created_at,
    );

    return rightTime - leftTime;
  });
}

function mapQueueEntries(entries: QueueEntry[]): BuildActivityItem[] {
  return entries.map((entry) => ({
    kind: "queue" as const,
    entry,
  }));
}

function withProjectJobNames<
  T extends { job_id?: string | null; job_name?: string | null },
>(items: T[], projectJobNames: Map<string, string>): T[] {
  return items.map((item) => {
    const currentName = item.job_name?.trim();
    const jobID = item.job_id?.trim();
    if (currentName || !jobID) {
      return item;
    }

    const resolvedName = projectJobNames.get(jobID);
    if (!resolvedName) {
      return item;
    }

    return {
      ...item,
      job_name: resolvedName,
    };
  });
}

function missingJobNameProjectIDs(
  items: Array<{
    project_id: string;
    job_id?: string | null;
    job_name?: string | null;
  }>,
): string[] {
  return [
    ...new Set(
      items
        .filter((item) => {
          const jobID = item.job_id?.trim();
          const jobName = item.job_name?.trim();
          return Boolean(item.project_id && jobID && !jobName);
        })
        .map((item) => item.project_id),
    ),
  ].sort();
}

function mapBuilds(builds: Build[]): BuildActivityItem[] {
  return builds.map((build) => ({
    kind: "build" as const,
    build,
  }));
}

function mapQueuedBuilds(builds: Build[]): QueueEntry[] {
  return builds
    .filter((build): build is Build & { status: QueueEntry["status"] } =>
      isQueueStatus(build.status),
    )
    .map((build) => ({
      build_id: build.id,
      build_number: build.build_number ?? 0,
      project_id: build.project_id,
      project_name: build.project_name,
      project_slug: build.project_slug,
      job_id: build.job_id,
      job_name: build.job_name,
      priority: build.priority,
      status: build.status,
      created_at: build.created_at,
      queued_at: build.queued_at,
      started_at: build.started_at,
      trigger_ref: build.trigger_ref,
      source_commit_sha: build.source_commit_sha,
      trigger_commit_sha: build.trigger_commit_sha,
    }));
}

function listQueueForScope(scope: BuildActivityScope): Promise<QueueEntry[]> {
  if (scope.type === "project") {
    return listQueue({ project_id: scope.projectId });
  }

  if (scope.type === "job") {
    // TODO: Replace this with a backend queue endpoint that supports job_id filtering.
    return listBuildsByJob(scope.jobId).then(mapQueuedBuilds);
  }

  return listQueue();
}

function listRecentBuildsForScope(
  scope: BuildActivityScope,
  limit?: number,
): Promise<Build[]> {
  if (scope.type === "project") {
    return listBuilds({ project_id: scope.projectId, limit });
  }
  if (scope.type === "job") {
    return listBuildsByJob(scope.jobId);
  }
  return listBuilds({ limit });
}

export function QueueActivityPanel({
  scope,
  title = "Queue activity",
  limit = DEFAULT_ACTIVITY_LIMIT,
  pollInterval,
}: {
  scope: BuildActivityScope;
  title?: string;
  limit?: number;
  pollInterval?: number | false;
}) {
  const projectID = scope.type === "project" ? scope.projectId : null;

  const { data: projectJobs } = useQuery({
    queryKey: ["projectJobs", projectID],
    queryFn: () =>
      projectID ? listJobsByProject(projectID) : Promise.resolve([]),
    enabled: Boolean(projectID),
  });

  const {
    data: jobQueueEntries,
    isLoading: jobQueueLoading,
    error: jobQueueError,
  } = useQuery({
    ...(scope.type === "job"
      ? jobBuildsQueryOptions(scope.jobId)
      : {
          queryKey: ["jobBuilds", "disabled", "queue"],
          queryFn: async () => [] as Build[],
          enabled: false,
        }),
    select: mapQueuedBuilds,
  });

  const {
    data: scopedQueueEntries,
    isLoading: scopedQueueLoading,
    error: scopedQueueError,
  } = useQuery({
    queryKey: ["activity", "queue", scopeQuerySuffix(scope), limit],
    queryFn: () => listQueueForScope(scope),
    enabled: scope.type !== "job",
    refetchInterval: (query) => {
      if (pollInterval === false || typeof pollInterval === "number") {
        return pollInterval;
      }

      const nextEntries = query.state.data as QueueEntry[] | undefined;
      if (!nextEntries || nextEntries.length === 0) {
        return SLOW_POLL_INTERVAL;
      }

      return nextEntries.some((entry) => isActiveBuild(entry.status))
        ? FAST_POLL_INTERVAL
        : SLOW_POLL_INTERVAL;
    },
  });

  const projectJobNames = new Map(
    (projectJobs ?? []).map((job) => [job.id, job.name]),
  );
  const queueEntries =
    scope.type === "job" ? jobQueueEntries : scopedQueueEntries;
  const isLoading = scope.type === "job" ? jobQueueLoading : scopedQueueLoading;
  const error = scope.type === "job" ? jobQueueError : scopedQueueError;
  const normalizedQueueEntries =
    scope.type === "project"
      ? withProjectJobNames(queueEntries ?? [], projectJobNames)
      : (queueEntries ?? []);

  return (
    <BuildActivityPanel
      title={title}
      items={mapQueueEntries(normalizedQueueEntries.slice(0, limit))}
      loadingMessage={isLoading ? "Loading queue…" : undefined}
      error={error}
      errorPrefix="Failed to load queue"
      emptyMessage="No builds in queue."
      contextMode={contextModeForScope(scope)}
    />
  );
}

export function RecentBuildsPanel({
  scope,
  title = "Recent builds",
  limit = DEFAULT_ACTIVITY_LIMIT,
  pollInterval,
}: {
  scope: BuildActivityScope;
  title?: string;
  limit?: number;
  pollInterval?: number | false;
}) {
  const projectID = scope.type === "project" ? scope.projectId : null;

  const { data: projectJobs } = useQuery({
    queryKey: ["projectJobs", projectID],
    queryFn: () =>
      projectID ? listJobsByProject(projectID) : Promise.resolve([]),
    enabled: Boolean(projectID),
  });

  const {
    data: jobBuilds,
    isLoading: jobBuildsLoading,
    error: jobBuildsError,
  } = useQuery(
    scope.type === "job"
      ? jobBuildsQueryOptions(scope.jobId)
      : {
          queryKey: ["jobBuilds", "disabled", "recent"],
          queryFn: async () => [] as Build[],
          enabled: false,
        },
  );

  const {
    data: scopedBuilds,
    isLoading: scopedBuildsLoading,
    error: scopedBuildsError,
  } = useQuery({
    queryKey: ["activity", "recent", scopeQuerySuffix(scope), limit],
    queryFn: () => listRecentBuildsForScope(scope, limit),
    enabled: scope.type !== "job",
    refetchInterval: (query) => {
      if (pollInterval === false || typeof pollInterval === "number") {
        return pollInterval;
      }

      const nextBuilds = query.state.data as Build[] | undefined;
      if (!nextBuilds || nextBuilds.length === 0) {
        return SLOW_POLL_INTERVAL;
      }

      return nextBuilds.some((build) => isActiveBuild(build.status))
        ? FAST_POLL_INTERVAL
        : SLOW_POLL_INTERVAL;
    },
  });

  const globalProjectIDs =
    scope.type === "global" ? missingJobNameProjectIDs(scopedBuilds ?? []) : [];
  const { data: globalProjectJobs } = useQuery({
    queryKey: [
      "activity",
      "recent",
      "global",
      "projectJobs",
      ...globalProjectIDs,
    ],
    queryFn: async () => {
      const responses = await Promise.all(
        globalProjectIDs.map((projectID) => listJobsByProject(projectID)),
      );
      return responses.flat();
    },
    enabled: scope.type === "global" && globalProjectIDs.length > 0,
  });

  const hydratedJobs =
    scope.type === "project" ? (projectJobs ?? []) : (globalProjectJobs ?? []);
  const projectJobNames = new Map(
    hydratedJobs.map((job) => [job.id, job.name]),
  );
  const builds = scope.type === "job" ? jobBuilds : scopedBuilds;
  const isLoading =
    scope.type === "job" ? jobBuildsLoading : scopedBuildsLoading;
  const error = scope.type === "job" ? jobBuildsError : scopedBuildsError;
  const normalizedBuilds =
    scope.type === "project" || scope.type === "global"
      ? withProjectJobNames(builds ?? [], projectJobNames)
      : (builds ?? []);

  const items = mapBuilds(sortByNewest(normalizedBuilds).slice(0, limit));

  return (
    <BuildActivityPanel
      title={title}
      items={items}
      loadingMessage={isLoading ? "Loading recent builds…" : undefined}
      error={error}
      errorPrefix="Failed to load builds"
      emptyMessage="No recent build activity."
      contextMode={contextModeForScope(scope)}
    />
  );
}

export function BuildActivityRail({
  scope,
  limit = DEFAULT_ACTIVITY_LIMIT,
  pollInterval,
}: {
  scope: BuildActivityScope;
  limit?: number;
  pollInterval?: number | false;
}) {
  return (
    <div className="dashboard-activity-column">
      <QueueActivityPanel
        scope={scope}
        limit={limit}
        pollInterval={pollInterval}
      />
      <RecentBuildsPanel
        scope={scope}
        limit={limit}
        pollInterval={pollInterval}
      />
    </div>
  );
}

export type { BuildActivityScope };
