import type { BuildStatus, BuildStepStatus, DataEnvelope } from "./build";

export interface PublicProject {
  id: string;
  slug: string;
  name: string;
  description?: string | null;
}

export interface PublicProjectListResponse {
  projects: PublicProject[];
}

export interface PublicBuildStep {
  index: number;
  name: string;
  status: BuildStepStatus;
  started_at?: string | null;
  completed_at?: string | null;
}

export interface PublicBuild {
  id: string;
  number: number;
  status: BuildStatus;
  job_name?: string | null;
  attempt: number;
  created_at: string;
  started_at?: string | null;
  completed_at?: string | null;
  steps?: PublicBuildStep[];
}

export interface PublicBuildListResponse {
  builds: PublicBuild[];
}

export type PublicEnvelope<T> = DataEnvelope<T>;
