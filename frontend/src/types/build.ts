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
  attempt_number?: number;
  rerun_of_build_id?: string | null;
  rerun_from_step_index?: number | null;
  execution_basis?: string | null;
  output_reuse_policy?: string | null;
  error_message: string | null;
  pipeline_source?: string | null;
  pipeline_path?: string | null;
  commit_sha?: string | null;
  ref?: string | null;
  repo_url?: string | null;
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
  job?: ExecutionJob | null;
  worker_id: string | null;
  started_at: string | null;
  finished_at: string | null;
  exit_code: number | null;
  stdout: string | null;
  stderr: string | null;
  error_message: string | null;
}

export interface ExecutionJobOutput {
  id: string;
  job_id: string;
  build_id: string;
  name: string;
  kind: string;
  declared_path: string;
  destination_uri?: string | null;
  content_type?: string | null;
  size_bytes?: number | null;
  digest?: string | null;
  status: 'declared' | 'available' | 'missing';
  created_at: string;
}

export interface ExecutionJob {
  id: string;
  build_id: string;
  step_id: string;
  name: string;
  step_index: number;
  attempt_number?: number;
  retry_of_job_id?: string | null;
  lineage_root_job_id?: string | null;
  status: 'queued' | 'running' | 'success' | 'failed';
  image: string;
  working_dir: string;
  command: string[];
  command_preview: string;
  environment: Record<string, string>;
  timeout_seconds?: number | null;
  pipeline_file_path?: string | null;
  context_dir?: string | null;
  source_repo_url?: string;
  source_commit_sha?: string;
  source_ref_name?: string | null;
  spec_version: number;
  spec_digest?: string | null;
  execution_basis?: string | null;
  created_at: string;
  started_at?: string | null;
  finished_at?: string | null;
  error_message?: string | null;
  outputs: ExecutionJobOutput[];
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

export interface RetryJobResponse {
  build: Build;
  job: ExecutionJob;
}

export interface RerunBuildFromStepRequest {
  step_index: number;
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
