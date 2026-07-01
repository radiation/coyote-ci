export type NotificationTargetType = "email" | "slack_webhook";

export type NotificationEventType = "build_succeeded" | "build_failed";

export interface NotificationTarget {
  id: string;
  owner_user_id?: string;
  type: NotificationTargetType;
  name: string;
  address?: string;
  webhook_configured: boolean;
  enabled: boolean;
  created_at: string;
  updated_at: string;
}

export interface NotificationTargetListResponse {
  targets: NotificationTarget[];
}

export interface MyEmailNotificationTargetResponse {
  target: NotificationTarget | null;
}

export interface UpdateMyEmailNotificationTargetRequest {
  enabled: boolean;
}

export interface CommitAuthorNotificationPreference {
  enabled: boolean;
  eligible: boolean;
  delivery_active: boolean;
  target: NotificationTarget | null;
  unavailable_reason?: string | null;
}

export type CommitAuthorFailureNotificationPreference =
  CommitAuthorNotificationPreference;

export type CommitAuthorSuccessNotificationPreference =
  CommitAuthorNotificationPreference;

export interface NotificationDefaults {
  default_commit_author_failure_email_enabled: boolean;
  default_commit_author_success_email_enabled: boolean;
}

export interface SlackWorkspaceIntegration {
  id: string;
  workspace_id: string;
  workspace_name?: string | null;
  workspace_url?: string | null;
  bot_id?: string | null;
  authed_user_id?: string | null;
  app_id?: string | null;
  linked_identity_count: number;
  enabled: boolean;
  connected_at: string;
  last_tested_at?: string | null;
  last_test_succeeded?: boolean | null;
  updated_at: string;
}

export interface SlackWorkspaceIntegrationStatus {
  configured: boolean;
  integration?: SlackWorkspaceIntegration | null;
}

export interface PutSlackWorkspaceIntegrationRequest {
  bot_token: string;
  replace_existing?: boolean;
}

export interface PatchSlackWorkspaceIntegrationRequest {
  enabled: boolean;
}

export interface SlackIdentityWorkspace {
  id: string;
  slack_workspace_id: string;
  name?: string | null;
  last_test_succeeded?: boolean | null;
}

export interface UserSlackIdentity {
  id: string;
  workspace: SlackIdentityWorkspace;
  slack_user_id: string;
  display_name?: string | null;
  real_name?: string | null;
  handle?: string | null;
  profile_image_url?: string | null;
  enabled: boolean;
  linked_at: string;
  last_verified_at?: string | null;
}

export interface MySlackIdentityResponse {
  workspace_status: string;
  workspace?: SlackIdentityWorkspace | null;
  identity?: UserSlackIdentity | null;
}

export interface ResolvedSlackIdentityCandidate {
  workspace: SlackIdentityWorkspace;
  slack_user_id: string;
  display_name?: string | null;
  real_name?: string | null;
  handle?: string | null;
  profile_image_url?: string | null;
}

export interface ResolveMySlackIdentityRequest {
  method: "authenticated_email";
}

export interface ResolveMySlackIdentityResponse {
  method: string;
  matched: boolean;
  candidate?: ResolvedSlackIdentityCandidate | null;
}

export interface CreateMySlackIdentityRequest {
  resolution_method: "authenticated_email";
  workspace_integration_id: string;
  slack_workspace_id: string;
  slack_user_id: string;
}

export interface PatchMySlackIdentityRequest {
  enabled: boolean;
}

export interface CreateEmailNotificationTargetRequest {
  type: "email";
  name: string;
  address: string;
  enabled?: boolean;
}

export interface CreateSlackNotificationTargetRequest {
  type: "slack_webhook";
  name: string;
  webhook_url: string;
  enabled?: boolean;
}

export type CreateNotificationTargetRequest =
  | CreateEmailNotificationTargetRequest
  | CreateSlackNotificationTargetRequest;

export interface UpdateNotificationTargetRequest {
  name?: string;
  address?: string;
  webhook_url?: string;
  enabled?: boolean;
}

export interface NotificationSubscription {
  id: string;
  target_id: string;
  project_id?: string | null;
  job_id?: string | null;
  event_type: NotificationEventType;
  enabled: boolean;
  created_at: string;
  updated_at: string;
}

export interface NotificationSubscriptionListResponse {
  subscriptions: NotificationSubscription[];
}

export interface CreateNotificationSubscriptionRequest {
  target_id: string;
  project_id?: string;
  job_id?: string;
  event_type: NotificationEventType;
  enabled?: boolean;
}

export interface UpdateNotificationSubscriptionRequest {
  enabled?: boolean;
}

export interface UpdateCommitAuthorFailureNotificationPreferenceRequest {
  enabled: boolean;
}

export interface UpdateCommitAuthorSuccessNotificationPreferenceRequest {
  enabled: boolean;
}

export interface UpdateNotificationDefaultsRequest {
  default_commit_author_failure_email_enabled: boolean;
  default_commit_author_success_email_enabled: boolean;
}
