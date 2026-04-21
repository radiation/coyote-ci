export type SourceCredentialKind = "https_token" | "ssh_key";

export interface SourceCredential {
  id: string;
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
