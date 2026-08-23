import { afterEach, describe, expect, it, vi } from "vitest";
import {
  getPublicBuild,
  getPublicProject,
  listPublicBuilds,
  listPublicProjects,
} from "./publicClient";

describe("publicClient", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("uses only the public project and build endpoints", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue({
      ok: true,
      json: async () => ({ data: { projects: [], builds: [] } }),
    } as Response);

    await listPublicProjects();
    await getPublicProject("platform project");
    await listPublicBuilds("platform project");
    await getPublicBuild("platform project", "build/1");

    expect(fetchMock.mock.calls.map(([url]) => url)).toEqual([
      "/api/public/projects",
      "/api/public/projects/platform%20project",
      "/api/public/projects/platform%20project/builds",
      "/api/public/projects/platform%20project/builds/build%2F1",
    ]);
  });
});
