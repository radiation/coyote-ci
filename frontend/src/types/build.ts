export interface VersionTag {
  id: string;
  job_id: string;
  version: string;
  target_type: string;
  artifact_id?: string | null;
  managed_image_version_id?: string | null;
  created_at: string;
}

export interface VersionTagCreateRequest {
  version: string;
  artifact_ids?: string[];
  managed_image_version_ids?: string[];
}

export interface JobVersionTagsResponse {
  job_id: string;
  version: string;
  tags: VersionTag[];
}

export interface ImageExecution {
  requested_ref?: string | null;
  resolved_ref?: string | null;
  source_kind: string;
  managed_image_id?: string | null;
  managed_image_version_id?: string | null;
  version_tags?: VersionTag[];
}

/** Matches the backend api.BuildResponse JSON shape. */
export interface Build {
  id: string;
  project_id: string;
  job_id?: string | null;
  status: BuildStatus;
  created_at: string;
  queued_at: string | null;
  started_at: string | null;
  finished_at: string | null;
  current_step_index: number;
  error_message: string | null;
  pipeline_source?: string | null;
  pipeline_path?: string | null;
  trigger_kind?: string | null;
  scm_provider?: string | null;
  event_type?: string | null;
  repository_owner?: string | null;
  repository_name?: string | null;
  repository_url?: string | null;
  trigger_ref?: string | null;
  ref_type?: string | null;
  source_commit_sha?: string | null;
  trigger_commit_sha?: string | null;
  actor?: string | null;
  image?: ImageExecution;
}

export type BuildStatus =
  | "pending"
  | "queued"
  | "preparing"
  | "running"
  | "success"
  | "failed";

/** Matches the backend api.BuildStepResponse JSON shape. */
export interface BuildStep {
  id: string;
  build_id: string;
  step_index: number;
  group_name?: string | null;
  name: string;
  command: string;
  status: BuildStepStatus;
  worker_id: string | null;
  started_at: string | null;
  finished_at: string | null;
  exit_code: number | null;
  stdout: string | null;
  stderr: string | null;
  error_message: string | null;
}

export type BuildStepStatus = "pending" | "running" | "success" | "failed";

/** Envelope: { data: { builds: Build[] } } */
export interface BuildListResponse {
  builds: Build[];
}

/** Envelope: { data: { build_id: string; steps: BuildStep[] } } */
export interface BuildStepsResponse {
  build_id: string;
  steps: BuildStep[];
}

export interface StepLogChunk {
  sequence_no: number;
  build_id: string;
  step_id: string;
  step_index: number;
  step_name: string;
  stream: "stdout" | "stderr" | "system";
  chunk_text: string;
  created_at: string;
}

export interface StepLogsResponse {
  build_id: string;
  step_index: number;
  after: number;
  next_sequence: number;
  chunks: StepLogChunk[];
}

/** Matches the backend api.BuildArtifactResponse JSON shape. */
export interface BuildArtifact {
  id: string;
  build_id: string;
  step_id: string | null;
  name?: string;
  path: string;
  size_bytes: number;
  content_type: string | null;
  checksum_sha256: string | null;
  storage_provider: string;
  download_url_path: string;
  version_tags?: VersionTag[];
  created_at: string;
}

/** Envelope: { data: { build_id: string; artifacts: BuildArtifact[] } } */
export interface BuildArtifactsResponse {
  build_id: string;
  artifacts: BuildArtifact[];
}

/** Generic envelope the backend wraps all responses in. */
export interface DataEnvelope<T> {
  data: T;
}
