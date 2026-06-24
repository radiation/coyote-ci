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
