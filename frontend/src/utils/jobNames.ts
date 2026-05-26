type JobNameHydrationItem = {
  project_id: string;
  job_id?: string | null;
  job_name?: string | null;
};

export function missingJobNameProjectIDs(
  items: JobNameHydrationItem[],
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

export function hydrateJobNames<
  T extends {
    job_id?: string | null;
    job_name?: string | null;
  },
>(items: T[], jobNames: Map<string, string>): T[] {
  return items.map((item) => {
    const currentName = item.job_name?.trim();
    const jobID = item.job_id?.trim();
    if (currentName || !jobID) {
      return item;
    }

    const resolvedName = jobNames.get(jobID);
    if (!resolvedName) {
      return item;
    }

    return {
      ...item,
      job_name: resolvedName,
    };
  });
}
