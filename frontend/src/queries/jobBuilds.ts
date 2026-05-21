import { queryOptions } from "@tanstack/react-query";
import { listBuildsByJob } from "../api";

export function jobBuildsQueryOptions(jobId: string) {
  return queryOptions({
    queryKey: ["jobBuilds", jobId],
    queryFn: () => listBuildsByJob(jobId),
    enabled: Boolean(jobId),
  });
}
