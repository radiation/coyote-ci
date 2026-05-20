import { useQuery } from "@tanstack/react-query";
import { listBuilds, listBuildsByJob, listQueue } from "../api";
import type { Build, QueueEntry } from "../types/build";
import {
  BuildActivityPanel,
  type BuildActivityItem,
} from "./BuildActivityList";

const DEFAULT_ACTIVITY_LIMIT = 6;

type BuildActivityScope =
  | { type: "global" }
  | { type: "project"; projectId: string }
  | { type: "job"; jobId: string };

function scopeQuerySuffix(scope: BuildActivityScope): string {
  if (scope.type === "global") {
    return "global";
  }
  if (scope.type === "project") {
    return `project:${scope.projectId}`;
  }
  return `job:${scope.jobId}`;
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

function mapBuilds(builds: Build[]): BuildActivityItem[] {
  return builds.map((build) => ({
    kind: "build" as const,
    build,
  }));
}

function listQueueForScope(scope: BuildActivityScope): Promise<QueueEntry[]> {
  if (scope.type === "project") {
    return listQueue({ project_id: scope.projectId });
  }

  if (scope.type === "job") {
    // TODO: Replace this with a backend queue endpoint that supports job_id filtering.
    return listBuildsByJob(scope.jobId).then((builds) =>
      builds
        .filter(
          (build) => build.status === "queued" || build.status === "running",
        )
        .map((build) => ({
          build_id: build.id,
          build_number: build.build_number ?? 0,
          project_id: build.project_id,
          project_name: build.project_name,
          project_slug: build.project_slug,
          job_id: build.job_id,
          priority: build.priority,
          status: build.status,
          created_at: build.created_at,
          queued_at: build.queued_at,
          started_at: build.started_at,
          trigger_ref: build.trigger_ref,
          source_commit_sha: build.source_commit_sha,
          trigger_commit_sha: build.trigger_commit_sha,
        })),
    );
  }

  return listQueue();
}

function listRecentBuildsForScope(scope: BuildActivityScope): Promise<Build[]> {
  if (scope.type === "project") {
    return listBuilds({ project_id: scope.projectId });
  }
  if (scope.type === "job") {
    return listBuildsByJob(scope.jobId);
  }
  return listBuilds();
}

export function QueueActivityPanel({
  scope,
  title = "Queue activity",
  limit = DEFAULT_ACTIVITY_LIMIT,
}: {
  scope: BuildActivityScope;
  title?: string;
  limit?: number;
}) {
  const {
    data: queueEntries,
    isLoading,
    error,
  } = useQuery({
    queryKey: ["activity", "queue", scopeQuerySuffix(scope), limit],
    queryFn: () => listQueueForScope(scope),
  });

  return (
    <BuildActivityPanel
      title={title}
      items={mapQueueEntries((queueEntries ?? []).slice(0, limit))}
      loadingMessage={isLoading ? "Loading queue…" : undefined}
      error={error}
      errorPrefix="Failed to load queue"
      emptyMessage="No builds in queue."
    />
  );
}

export function RecentBuildsPanel({
  scope,
  title = "Recent builds",
  limit = DEFAULT_ACTIVITY_LIMIT,
}: {
  scope: BuildActivityScope;
  title?: string;
  limit?: number;
}) {
  const {
    data: builds,
    isLoading,
    error,
  } = useQuery({
    queryKey: ["activity", "recent", scopeQuerySuffix(scope), limit],
    queryFn: () => listRecentBuildsForScope(scope),
  });

  const items = mapBuilds(sortByNewest(builds ?? []).slice(0, limit));

  return (
    <BuildActivityPanel
      title={title}
      items={items}
      loadingMessage={isLoading ? "Loading recent builds…" : undefined}
      error={error}
      errorPrefix="Failed to load builds"
      emptyMessage="No recent build activity."
    />
  );
}

export function BuildActivityRail({
  scope,
  limit = DEFAULT_ACTIVITY_LIMIT,
}: {
  scope: BuildActivityScope;
  limit?: number;
}) {
  return (
    <div className="dashboard-activity-column">
      <QueueActivityPanel scope={scope} limit={limit} />
      <RecentBuildsPanel scope={scope} limit={limit} />
    </div>
  );
}

export type { BuildActivityScope };
