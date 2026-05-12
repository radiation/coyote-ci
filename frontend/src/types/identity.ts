export type GlobalRole = "admin" | "user";
export type ProjectMemberRole = "owner" | "maintainer" | "viewer";

export interface User {
  id: string;
  email: string;
  display_name?: string | null;
  global_role: GlobalRole;
  created_at?: string;
  updated_at?: string;
}

export interface UserListResponse {
  users: User[];
}

export interface MeResponse {
  auth_mode: "disabled" | "header" | "oidc";
  user: User;
}

export interface AuthConfigResponse {
  auth_mode: "disabled" | "header" | "oidc";
  login_url: string | null;
}

export interface CreateUserRequest {
  email: string;
  display_name?: string;
  global_role?: GlobalRole;
}

export interface UpdateUserRequest {
  email?: string;
  display_name?: string | null;
  global_role?: GlobalRole;
}

export interface ProjectMember {
  project_id: string;
  user_id: string;
  email?: string;
  display_name?: string | null;
  role: ProjectMemberRole;
  created_at: string;
  updated_at: string;
}

export interface ProjectMemberListResponse {
  members: ProjectMember[];
}

export interface UpsertProjectMemberRequest {
  role: ProjectMemberRole;
}
