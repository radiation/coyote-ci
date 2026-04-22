export interface JobManagedImageConfig {
  enabled: boolean;
  managed_image_name: string;
  pipeline_path: string;
  write_credential_id: string;
  bot_branch_prefix: string;
  commit_author_name: string;
  commit_author_email: string;
  created_at: string;
  updated_at: string;
}

export interface Job {
  id: string;
  project_id: string;
  name: string;
  repository_url: string;
  default_ref: string;
  push_enabled: boolean;
  push_branch?: string | null;
  pipeline_yaml: string;
  pipeline_path?: string | null;
  managed_image?: JobManagedImageConfig | null;
  enabled: boolean;
  created_at: string;
  updated_at: string;
}

export interface CreateJobManagedImageConfigRequest {
  enabled: boolean;
  managed_image_name: string;
  pipeline_path: string;
  write_credential_id: string;
  bot_branch_prefix?: string;
  commit_author_name?: string;
  commit_author_email?: string;
}

export interface UpdateJobManagedImageConfigRequest {
  enabled?: boolean;
  managed_image_name?: string;
  pipeline_path?: string;
  write_credential_id?: string;
  bot_branch_prefix?: string;
  commit_author_name?: string;
  commit_author_email?: string;
}

export interface JobListResponse {
  jobs: Job[];
}

export interface CreateJobRequest {
  project_id: string;
  name: string;
  repository_url: string;
  default_ref: string;
  push_enabled?: boolean;
  push_branch?: string;
  pipeline_yaml?: string;
  pipeline_path?: string;
  managed_image?: CreateJobManagedImageConfigRequest;
  enabled?: boolean;
}

export interface UpdateJobRequest {
  name?: string;
  repository_url?: string;
  default_ref?: string;
  push_enabled?: boolean;
  push_branch?: string;
  pipeline_yaml?: string;
  pipeline_path?: string;
  managed_image?: UpdateJobManagedImageConfigRequest | null;
  enabled?: boolean;
}
