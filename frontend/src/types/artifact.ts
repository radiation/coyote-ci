import type { BuildStatus, VersionTag } from "./build";

export type ArtifactType =
  | "docker_image"
  | "npm_package"
  | "generic"
  | "unknown";

export interface ArtifactLineage {
  project_id: string;
  project_name?: string | null;
  job_id?: string | null;
  job_name?: string | null;
  build_id: string;
  build_number: number;
  artifact_id: string;
  artifact_name?: string;
  artifact_path: string;
  versions?: string[];
  channels?: string[];
  git_ref?: string | null;
  git_sha?: string | null;
  created_at: string;
}

export interface ArtifactBrowseVersion {
  artifact_id: string;
  name?: string;
  build_id: string;
  build_number: number;
  build_status: BuildStatus;
  project_id: string;
  project_name?: string | null;
  project_slug?: string | null;
  job_id?: string | null;
  job_name?: string | null;
  step_id?: string | null;
  step_index?: number | null;
  step_name?: string | null;
  path: string;
  size_bytes: number;
  content_type: string | null;
  checksum_sha256: string | null;
  storage_provider: string;
  download_url_path: string;
  version_tags?: VersionTag[];
  lineage?: ArtifactLineage | null;
  created_at: string;
}

export interface ArtifactBrowseItem {
  key: string;
  name?: string;
  path: string;
  project_id: string;
  project_name?: string | null;
  project_slug?: string | null;
  job_id?: string | null;
  job_name?: string | null;
  artifact_type: ArtifactType;
  latest_created_at: string;
  versions: ArtifactBrowseVersion[];
}

export interface ArtifactBrowseResponse {
  artifacts: ArtifactBrowseItem[];
}

export interface ArtifactCatalogItem {
  id: string;
  name?: string;
  path: string;
  artifact_type: ArtifactType;
  build_id: string;
  build_number: number;
  build_status: BuildStatus;
  project_id: string;
  project_name?: string | null;
  project_slug?: string | null;
  job_id?: string | null;
  job_name?: string | null;
  step_id?: string | null;
  step_index?: number | null;
  step_name?: string | null;
  size_bytes: number;
  content_type: string | null;
  checksum_sha256: string | null;
  storage_provider: string;
  download_url_path: string;
  created_at: string;
}

export interface ArtifactCatalogResponse {
  artifacts: ArtifactCatalogItem[];
}

export interface ArtifactDetail extends ArtifactCatalogItem {
  version_tags?: VersionTag[];
  lineage?: ArtifactLineage | null;
}

export interface DataEnvelope<T> {
  data: T;
}
