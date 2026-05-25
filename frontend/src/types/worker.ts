export type WorkerStatus = "idle" | "busy" | "stale";

export interface Worker {
  id: string;
  name: string;
  status: WorkerStatus;
  last_heartbeat_at: string;
  created_at: string;
  updated_at: string;
  current_build_id?: string | null;
  current_build_number?: number | null;
  current_step_id?: string | null;
  current_step_index?: number | null;
  current_step_name?: string | null;
  lease_expires_at?: string | null;
  claimed_at?: string | null;
  project_id?: string | null;
  project_name?: string | null;
  project_slug?: string | null;
  job_id?: string | null;
  job_name?: string | null;
  stale_lease: boolean;
  stale_heartbeat: boolean;
}

export interface WorkerListResponse {
  workers: Worker[];
}
