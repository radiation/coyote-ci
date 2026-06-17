export type NotificationTargetType = "email" | "slack_webhook";

export type NotificationEventType = "build_succeeded" | "build_failed";

export interface NotificationTarget {
  id: string;
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

export interface CreateNotificationTargetRequest {
  type?: NotificationTargetType;
  name: string;
  address?: string;
  webhook_url?: string;
  enabled?: boolean;
}

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
