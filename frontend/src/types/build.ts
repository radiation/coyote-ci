/** Matches the backend api.BuildResponse JSON shape. */
export interface Build {
  id: string;
  project_id: string;
  status: BuildStatus;
  created_at: string;
  queued_at: string | null;
  started_at: string | null;
  finished_at: string | null;
  current_step_index: number;
  error_message: string | null;
  pipeline_source?: string | null;
  pipeline_path?: string | null;
}

export type BuildStatus = 'pending' | 'queued' | 'running' | 'success' | 'failed';

/** Matches the backend api.BuildStepResponse JSON shape. */
export interface BuildStep {
  id: string;
  build_id: string;
  step_index: number;
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

export type BuildStepStatus = 'pending' | 'running' | 'success' | 'failed';

/** Matches the backend api.CreateBuildStepInput JSON shape. */
export interface CreateBuildStepInput {
  name: string;
  command?: string;
  args?: string[];
  env?: Record<string, string>;
  working_dir?: string;
  timeout_seconds?: number;
}

/** Matches the backend api.CreateBuildRequest JSON shape. */
export interface CreateBuildRequest {
  project_id: string;
  template?: BuildTemplate;
  steps?: CreateBuildStepInput[];
}

export type BuildTemplate = 'default' | 'test' | 'build' | 'custom' | 'fail' | 'pipeline' | 'repo';

/** Matches the backend api.CreatePipelineBuildRequest JSON shape. */
export interface CreatePipelineBuildRequest {
  project_id: string;
  pipeline_yaml: string;
}

/** Matches the backend api.CreateRepoBuildRequest JSON shape. */
export interface CreateRepoBuildRequest {
  project_id: string;
  repo_url: string;
  ref: string;
  pipeline_path?: string;
}

/** Matches backend api.QueueBuildStepInput JSON shape. */
export interface QueueBuildStepInput {
  name?: string;
  command: string;
}

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
  stream: 'stdout' | 'stderr' | 'system';
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
  path: string;
  size_bytes: number;
  content_type: string | null;
  checksum_sha256: string | null;
  download_url_path: string;
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
