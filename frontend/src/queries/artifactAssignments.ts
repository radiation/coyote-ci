import type { QueryClient } from "@tanstack/react-query";

export function invalidateArtifactAssignment(
  client: QueryClient,
  artifactID: string,
  buildID: string,
  kind: "version" | "channel",
) {
  const artifactScope = kind === "channel" ? [] : [artifactID];
  const buildScope = kind === "channel" ? [] : [buildID];
  return Promise.all(
    [
      ["artifact", ...artifactScope],
      ["buildArtifacts", ...buildScope],
      ["jobLatestSuccessfulArtifacts", ...buildScope],
      ["artifactLogicalBrowse"],
      ["artifactCatalog"],
    ].map((queryKey) => client.invalidateQueries({ queryKey })),
  );
}
