import type {
  PublicBuild,
  PublicBuildListResponse,
  PublicEnvelope,
  PublicProject,
  PublicProjectListResponse,
} from "../types/public";
import { fetchJSON } from "./request";

export async function listPublicProjects(): Promise<PublicProject[]> {
  const envelope =
    await fetchJSON<PublicEnvelope<PublicProjectListResponse>>(
      "/public/projects",
    );
  return envelope.data.projects;
}

export async function getPublicProject(slug: string): Promise<PublicProject> {
  const envelope = await fetchJSON<PublicEnvelope<PublicProject>>(
    `/public/projects/${encodeURIComponent(slug)}`,
  );
  return envelope.data;
}

export async function listPublicBuilds(slug: string): Promise<PublicBuild[]> {
  const envelope = await fetchJSON<PublicEnvelope<PublicBuildListResponse>>(
    `/public/projects/${encodeURIComponent(slug)}/builds`,
  );
  return envelope.data.builds;
}

export async function getPublicBuild(
  slug: string,
  buildID: string,
): Promise<PublicBuild> {
  const envelope = await fetchJSON<PublicEnvelope<PublicBuild>>(
    `/public/projects/${encodeURIComponent(slug)}/builds/${encodeURIComponent(buildID)}`,
  );
  return envelope.data;
}
