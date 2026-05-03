import type { Job } from "./job";

export interface Project {
  id: string;
  name: string;
  slug: string;
  description?: string | null;
  created_at: string;
  updated_at: string;
}

export interface ProjectListResponse {
  projects: Project[];
}

export interface CreateProjectRequest {
  name: string;
  slug: string;
  description?: string;
}

export interface UpdateProjectRequest {
  name?: string;
  slug?: string;
  description?: string | null;
}

export interface ProjectJobsResponse {
  jobs: Job[];
}
