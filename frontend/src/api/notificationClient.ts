import type { DataEnvelope } from "../types/build";
import type {
  CreateMySlackIdentityRequest,
  PatchSlackWorkspaceIntegrationRequest,
  PatchMySlackIdentityRequest,
  PutSlackWorkspaceIntegrationRequest,
  CommitAuthorFailureNotificationPreference,
  CommitAuthorSuccessNotificationPreference,
  CreateNotificationSubscriptionRequest,
  CreateNotificationTargetRequest,
  MySlackIdentityResponse,
  MyEmailNotificationTargetResponse,
  NotificationDefaults,
  NotificationSubscription,
  NotificationSubscriptionListResponse,
  NotificationTarget,
  NotificationTargetListResponse,
  ResolveMySlackIdentityRequest,
  ResolveMySlackIdentityResponse,
  SlackWorkspaceIntegrationStatus,
  UpdateCommitAuthorFailureNotificationPreferenceRequest,
  UpdateCommitAuthorSuccessNotificationPreferenceRequest,
  UpdateMyEmailNotificationTargetRequest,
  UpdateNotificationDefaultsRequest,
  UpdateNotificationSubscriptionRequest,
  UpdateNotificationTargetRequest,
  UserSlackIdentity,
} from "../types/notification";
import {
  deleteNoContent,
  fetchJSON,
  postJSON,
  postNoBodyJSON,
} from "./request";

export async function listNotificationTargets(): Promise<NotificationTarget[]> {
  const envelope = await fetchJSON<
    DataEnvelope<NotificationTargetListResponse>
  >("/notification-targets");
  return envelope.data.targets;
}

export async function getMyEmailNotificationTarget(): Promise<NotificationTarget | null> {
  const envelope = await fetchJSON<
    DataEnvelope<MyEmailNotificationTargetResponse>
  >("/me/notification-targets/email");
  return envelope.data.target;
}

export async function ensureMyEmailNotificationTarget(): Promise<NotificationTarget> {
  const envelope = await postNoBodyJSON<DataEnvelope<NotificationTarget>>(
    "/me/notification-targets/email",
  );
  return envelope.data;
}

export async function setMyEmailNotificationTargetEnabled(
  input: UpdateMyEmailNotificationTargetRequest,
): Promise<NotificationTarget> {
  const envelope = await fetchJSON<DataEnvelope<NotificationTarget>>(
    "/me/notification-targets/email",
    {
      method: "PUT",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(input),
    },
  );
  return envelope.data;
}

export async function getCommitAuthorFailureNotificationPreference(): Promise<CommitAuthorFailureNotificationPreference> {
  const envelope = await fetchJSON<
    DataEnvelope<CommitAuthorFailureNotificationPreference>
  >("/me/notification-preferences/commit-author-failures");
  return envelope.data;
}

export async function getCommitAuthorSuccessNotificationPreference(): Promise<CommitAuthorSuccessNotificationPreference> {
  const envelope = await fetchJSON<
    DataEnvelope<CommitAuthorSuccessNotificationPreference>
  >("/me/notification-preferences/commit-author-successes");
  return envelope.data;
}

export async function getNotificationDefaults(): Promise<NotificationDefaults> {
  const envelope = await fetchJSON<DataEnvelope<NotificationDefaults>>(
    "/settings/notifications/defaults",
  );
  return envelope.data;
}

export async function setCommitAuthorFailureNotificationPreference(
  input: UpdateCommitAuthorFailureNotificationPreferenceRequest,
): Promise<CommitAuthorFailureNotificationPreference> {
  const envelope = await fetchJSON<
    DataEnvelope<CommitAuthorFailureNotificationPreference>
  >("/me/notification-preferences/commit-author-failures", {
    method: "PUT",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(input),
  });
  return envelope.data;
}

export async function setCommitAuthorSuccessNotificationPreference(
  input: UpdateCommitAuthorSuccessNotificationPreferenceRequest,
): Promise<CommitAuthorSuccessNotificationPreference> {
  const envelope = await fetchJSON<
    DataEnvelope<CommitAuthorSuccessNotificationPreference>
  >("/me/notification-preferences/commit-author-successes", {
    method: "PUT",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(input),
  });
  return envelope.data;
}

export async function setNotificationDefaults(
  input: UpdateNotificationDefaultsRequest,
): Promise<NotificationDefaults> {
  const envelope = await fetchJSON<DataEnvelope<NotificationDefaults>>(
    "/settings/notifications/defaults",
    {
      method: "PUT",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(input),
    },
  );
  return envelope.data;
}

export async function getSlackWorkspaceIntegration(): Promise<SlackWorkspaceIntegrationStatus> {
  const envelope = await fetchJSON<
    DataEnvelope<SlackWorkspaceIntegrationStatus>
  >("/settings/integrations/slack");
  return envelope.data;
}

export async function getMySlackIdentity(): Promise<MySlackIdentityResponse> {
  const envelope =
    await fetchJSON<DataEnvelope<MySlackIdentityResponse>>(
      "/me/slack-identity",
    );
  return envelope.data;
}

export async function resolveMySlackIdentity(
  input: ResolveMySlackIdentityRequest,
): Promise<ResolveMySlackIdentityResponse> {
  const envelope = await fetchJSON<
    DataEnvelope<ResolveMySlackIdentityResponse>
  >("/me/slack-identity/resolve", {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(input),
  });
  return envelope.data;
}

export async function createMySlackIdentity(
  input: CreateMySlackIdentityRequest,
): Promise<UserSlackIdentity> {
  const envelope = await fetchJSON<DataEnvelope<UserSlackIdentity>>(
    "/me/slack-identity",
    {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(input),
    },
  );
  return envelope.data;
}

export async function patchMySlackIdentity(
  input: PatchMySlackIdentityRequest,
): Promise<UserSlackIdentity> {
  const envelope = await fetchJSON<DataEnvelope<UserSlackIdentity>>(
    "/me/slack-identity",
    {
      method: "PATCH",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(input),
    },
  );
  return envelope.data;
}

export async function deleteMySlackIdentity(): Promise<void> {
  await deleteNoContent("/me/slack-identity");
}

export async function putSlackWorkspaceIntegration(
  input: PutSlackWorkspaceIntegrationRequest,
): Promise<SlackWorkspaceIntegrationStatus> {
  const envelope = await fetchJSON<
    DataEnvelope<SlackWorkspaceIntegrationStatus>
  >("/settings/integrations/slack", {
    method: "PUT",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(input),
  });
  return envelope.data;
}

export async function patchSlackWorkspaceIntegration(
  input: PatchSlackWorkspaceIntegrationRequest,
): Promise<SlackWorkspaceIntegrationStatus> {
  const envelope = await fetchJSON<
    DataEnvelope<SlackWorkspaceIntegrationStatus>
  >("/settings/integrations/slack", {
    method: "PATCH",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(input),
  });
  return envelope.data;
}

export async function testSlackWorkspaceIntegration(): Promise<SlackWorkspaceIntegrationStatus> {
  const envelope = await postNoBodyJSON<
    DataEnvelope<SlackWorkspaceIntegrationStatus>
  >("/settings/integrations/slack/test");
  return envelope.data;
}

export async function deleteSlackWorkspaceIntegration(): Promise<void> {
  await deleteNoContent("/settings/integrations/slack");
}

export async function createNotificationTarget(
  input: CreateNotificationTargetRequest,
): Promise<NotificationTarget> {
  const envelope = await postJSON<
    DataEnvelope<NotificationTarget>,
    CreateNotificationTargetRequest
  >("/notification-targets", input);
  return envelope.data;
}

export async function updateNotificationTarget(
  id: string,
  input: UpdateNotificationTargetRequest,
): Promise<NotificationTarget> {
  const envelope = await fetchJSON<DataEnvelope<NotificationTarget>>(
    `/notification-targets/${encodeURIComponent(id)}`,
    {
      method: "PATCH",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(input),
    },
  );
  return envelope.data;
}

export async function listNotificationSubscriptions(input?: {
  project_id?: string;
  job_id?: string;
}): Promise<NotificationSubscription[]> {
  const params = new URLSearchParams();
  const projectID = input?.project_id?.trim() ?? "";
  const jobID = input?.job_id?.trim() ?? "";

  if (projectID) {
    params.set("project_id", projectID);
  }
  if (jobID) {
    params.set("job_id", jobID);
  }

  const suffix = params.size > 0 ? `?${params.toString()}` : "";
  const envelope = await fetchJSON<
    DataEnvelope<NotificationSubscriptionListResponse>
  >(`/notification-subscriptions${suffix}`);
  return envelope.data.subscriptions;
}

export async function createNotificationSubscription(
  input: CreateNotificationSubscriptionRequest,
): Promise<NotificationSubscription> {
  const envelope = await postJSON<
    DataEnvelope<NotificationSubscription>,
    CreateNotificationSubscriptionRequest
  >("/notification-subscriptions", input);
  return envelope.data;
}

export async function updateNotificationSubscription(
  id: string,
  input: UpdateNotificationSubscriptionRequest,
): Promise<NotificationSubscription> {
  const envelope = await fetchJSON<DataEnvelope<NotificationSubscription>>(
    `/notification-subscriptions/${encodeURIComponent(id)}`,
    {
      method: "PATCH",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(input),
    },
  );
  return envelope.data;
}

export async function deleteNotificationSubscription(
  id: string,
): Promise<void> {
  await deleteNoContent(
    `/notification-subscriptions/${encodeURIComponent(id)}`,
  );
}
