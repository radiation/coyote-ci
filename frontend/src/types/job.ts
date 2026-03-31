export interface Job {
  id: string;
  project_id: string;
  name: string;
  repository_url: string;
  default_ref: string;
  push_enabled: boolean;
  push_branch?: string | null;
  pipeline_yaml: string;
  enabled: boolean;
  created_at: string;
  updated_at: string;
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
  pipeline_yaml: string;
  enabled?: boolean;
}

export interface UpdateJobRequest {
  name?: string;
  repository_url?: string;
  default_ref?: string;
  push_enabled?: boolean;
  push_branch?: string;
  pipeline_yaml?: string;
  enabled?: boolean;
}
