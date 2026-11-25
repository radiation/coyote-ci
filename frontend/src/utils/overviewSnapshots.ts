import { matchQuery, QueryCache } from "@tanstack/react-query";

export const overviewKeys = {
  queue: ["queue", "", undefined],
  history: ["queue-page", "recent-builds", ""],
  workers: ["workers"],
} as const;

export function denied(error: unknown) {
  return (
    error instanceof Error &&
    "status" in error &&
    (error.status === 401 || error.status === 403)
  );
}

export function createOverviewAwareQueryCache() {
  return new QueryCache({
    onError(error, query) {
      if (
        denied(error) &&
        Object.values(overviewKeys).some((queryKey) =>
          matchQuery({ queryKey, exact: true }, query),
        )
      ) {
        query.setState({ data: undefined, dataUpdatedAt: 0 });
      }
    },
  });
}
