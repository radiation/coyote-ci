import type { BuildStatus, VersionTag } from "./build";

export type ArtifactType =
  | "docker_image"
  | "npm_package"
  | "generic"
  | "unknown";

export interface ArtifactBrowseVersion {
  artifact_id: string;
  name?: string;
  build_id: string;
  build_number: number;
  build_status: BuildStatus;
  project_id: string;
  job_id?: string | null;
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
  created_at: string;
}

export interface ArtifactBrowseItem {
  key: string;
  name?: string;
  path: string;
  project_id: string;
  job_id?: string | null;
  artifact_type: ArtifactType;
  latest_created_at: string;
  versions: ArtifactBrowseVersion[];
}

export interface ArtifactBrowseResponse {
  artifacts: ArtifactBrowseItem[];
}

export interface DataEnvelope<T> {
  data: T;
}
