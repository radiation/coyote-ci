export type SourceCredentialKind = "https_token" | "ssh_key";

export interface SourceCredential {
  id: string;
  project_id: string;
  name: string;
  kind: SourceCredentialKind;
  username?: string | null;
  secret_ref: string;
  created_at: string;
  updated_at: string;
}

export interface SourceCredentialListResponse {
  credentials: SourceCredential[];
}

export interface CreateSourceCredentialRequest {
  project_id: string;
  name: string;
  kind: SourceCredentialKind;
  username?: string;
  secret_ref: string;
}

export interface UpdateSourceCredentialRequest {
  name?: string;
  kind?: SourceCredentialKind;
  username?: string | null;
  secret_ref?: string;
}

export interface RepoWritebackConfig {
  id: string;
  project_id: string;
  repository_url: string;
  pipeline_path: string;
  managed_image_name: string;
  write_credential_id: string;
  bot_branch_prefix: string;
  commit_author_name: string;
  commit_author_email: string;
  enabled: boolean;
  created_at: string;
  updated_at: string;
}

export interface RepoWritebackConfigListResponse {
  configs: RepoWritebackConfig[];
}

export interface CreateRepoWritebackConfigRequest {
  project_id: string;
  repository_url: string;
  pipeline_path: string;
  managed_image_name: string;
  write_credential_id: string;
  bot_branch_prefix?: string;
  commit_author_name?: string;
  commit_author_email?: string;
  enabled?: boolean;
}

export interface UpdateRepoWritebackConfigRequest {
  repository_url?: string;
  pipeline_path?: string;
  managed_image_name?: string;
  write_credential_id?: string;
  bot_branch_prefix?: string;
  commit_author_name?: string;
  commit_author_email?: string;
  enabled?: boolean;
}
