import type { Job } from "../types/job";
import type { Project } from "../types/project";
import type {
  NotificationEventType,
  NotificationSubscription,
  NotificationTarget,
  NotificationTargetType,
  SlackWorkspaceIntegration,
} from "../types/notification";

export const EVENT_OPTIONS: NotificationEventType[] = [
  "build_failed",
  "build_succeeded",
];

export type NotificationScopeType = "project" | "job";

export type EventSelection = Record<NotificationEventType, boolean>;

export type RuleEnabledState = "enabled" | "disabled" | "mixed";

export interface GroupedSubscriptionRule {
  key: string;
  targetID: string;
  scopeType: NotificationScopeType;
  projectID?: string;
  jobID?: string;
  subscriptions: NotificationSubscription[];
  eventTypes: NotificationEventType[];
  enabledState: RuleEnabledState;
  updatedAt: string;
}

export interface RuleDraft {
  targetID: string;
  scopeType: NotificationScopeType | "";
  projectID: string;
  jobID: string;
  events: EventSelection;
  enabled: boolean;
}

export interface ReconcilePlan {
  createEvents: NotificationEventType[];
  updateEnabledRows: NotificationSubscription[];
  deleteRows: NotificationSubscription[];
}

interface RuleIdentity {
  targetID: string;
  scopeType: NotificationScopeType;
  projectID?: string;
  jobID?: string;
}

export const DEFAULT_EVENT_SELECTION: EventSelection = {
  build_failed: true,
  build_succeeded: false,
};

export function slackWorkspaceDisplayName(
  integration: SlackWorkspaceIntegration,
): string {
  return integration.workspace_name?.trim() || integration.workspace_id;
}

export function slackWorkspaceLink(
  workspaceURL: string | null | undefined,
): { href: string; label: string } | null {
  const trimmed = workspaceURL?.trim();
  if (!trimmed) {
    return null;
  }

  try {
    const parsed = new URL(trimmed);
    if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
      return null;
    }
    return {
      href: trimmed,
      label: parsed.host,
    };
  } catch {
    return null;
  }
}

export function slackConnectionStatus(integration: SlackWorkspaceIntegration): {
  label: string;
  className: string;
} {
  if (integration.enabled) {
    return { label: "Connected", className: "status-success" };
  }
  return { label: "Disabled", className: "status-canceled" };
}

export function slackTestStatus(integration: SlackWorkspaceIntegration): {
  label: string;
  className: string;
} {
  if (integration.last_test_succeeded === true) {
    return { label: "Passed", className: "status-success" };
  }
  if (integration.last_test_succeeded === false) {
    return { label: "Failed", className: "status-failed" };
  }
  return { label: "Not tested", className: "status-canceled" };
}

export function slackLinkedIdentitySummary(
  integration: SlackWorkspaceIntegration,
): string {
  if (integration.linked_identity_count === 1) {
    return "1 linked personal Slack identity";
  }
  return `${integration.linked_identity_count} linked personal Slack identities`;
}

export function groupSubscriptions(
  subscriptions: NotificationSubscription[],
): GroupedSubscriptionRule[] {
  const grouped = new Map<string, NotificationSubscription[]>();
  for (const subscription of subscriptions) {
    const key = ruleKeyFromRow(subscription);
    const current = grouped.get(key);
    if (current) {
      current.push(subscription);
    } else {
      grouped.set(key, [subscription]);
    }
  }

  const rules: GroupedSubscriptionRule[] = [];
  grouped.forEach((rows, key) => {
    if (rows.length === 0) {
      return;
    }
    const first = rows[0];
    const scopeType: NotificationScopeType = first.project_id
      ? "project"
      : "job";

    const eventTypes: NotificationEventType[] = [];
    const seenEvents = new Set<NotificationEventType>();
    let enabledCount = 0;
    let latestUpdatedAt = first.updated_at;

    for (const row of rows) {
      if (!seenEvents.has(row.event_type)) {
        seenEvents.add(row.event_type);
        eventTypes.push(row.event_type);
      }
      if (row.enabled) {
        enabledCount += 1;
      }
      if (row.updated_at > latestUpdatedAt) {
        latestUpdatedAt = row.updated_at;
      }
    }

    eventTypes.sort(
      (left, right) =>
        EVENT_OPTIONS.indexOf(left) - EVENT_OPTIONS.indexOf(right),
    );

    const enabledState: RuleEnabledState =
      enabledCount === rows.length
        ? "enabled"
        : enabledCount === 0
          ? "disabled"
          : "mixed";

    rules.push({
      key,
      targetID: first.target_id,
      scopeType,
      projectID: first.project_id ?? undefined,
      jobID: first.job_id ?? undefined,
      subscriptions: rows,
      eventTypes,
      enabledState,
      updatedAt: latestUpdatedAt,
    });
  });

  rules.sort((left, right) => right.updatedAt.localeCompare(left.updatedAt));
  return rules;
}

export function buildReconcilePlan(
  rule: GroupedSubscriptionRule,
  draft: RuleDraft,
): ReconcilePlan {
  const desiredEvents = selectedEventsFromDraft(draft);
  const desiredIdentity = draftToIdentity(draft);

  const byDesiredKey = new Map<string, NotificationSubscription[]>();
  for (const row of rule.subscriptions) {
    const key = desiredKeyFromRow(row);
    const current = byDesiredKey.get(key);
    if (current) {
      current.push(row);
    } else {
      byDesiredKey.set(key, [row]);
    }
  }

  const createEvents: NotificationEventType[] = [];
  const updateEnabledRows: NotificationSubscription[] = [];
  const deleteRows: NotificationSubscription[] = [];
  const keptRowIDs = new Set<string>();

  for (const eventType of desiredEvents) {
    const key = desiredKeyFromIdentityAndEvent(desiredIdentity, eventType);
    const matches = byDesiredKey.get(key) ?? [];
    if (matches.length === 0) {
      createEvents.push(eventType);
      continue;
    }

    const [keeper, ...duplicates] = matches;
    keptRowIDs.add(keeper.id);
    if (keeper.enabled !== draft.enabled) {
      updateEnabledRows.push(keeper);
    }
    for (const duplicate of duplicates) {
      deleteRows.push(duplicate);
    }
  }

  for (const row of rule.subscriptions) {
    if (
      !keptRowIDs.has(row.id) &&
      !deleteRows.find((candidate) => candidate.id === row.id)
    ) {
      deleteRows.push(row);
    }
  }

  return {
    createEvents,
    updateEnabledRows,
    deleteRows,
  };
}

export function selectedEventsFromDraft(
  draft: RuleDraft,
): NotificationEventType[] {
  return EVENT_OPTIONS.filter((eventType) => draft.events[eventType]);
}

export function validateRuleDraft(draft: RuleDraft): string | null {
  if (!draft.targetID.trim()) {
    return "Notification target is required.";
  }
  if (!draft.scopeType) {
    return "Scope is required.";
  }
  if (!draft.projectID.trim()) {
    return "Project is required.";
  }
  if (draft.scopeType === "job" && !draft.jobID.trim()) {
    return "Job is required for job scope.";
  }
  return null;
}

export function formatTargetTypeLabel(type: NotificationTargetType): string {
  return type === "slack_webhook" ? "Slack" : "Email";
}

export function formatTargetSelectorLabel(target: NotificationTarget): string {
  return `${formatTargetTypeLabel(target.type)} · ${target.name}`;
}

export function formatEventLabel(eventType: NotificationEventType): string {
  return eventType === "build_failed" ? "Build failed" : "Build succeeded";
}

export function formatRuleEnabledState(state: RuleEnabledState): string {
  if (state === "mixed") {
    return "Mixed";
  }
  return state === "enabled" ? "Enabled" : "Disabled";
}

export function describeScope(
  rule: GroupedSubscriptionRule,
  projectLookup: Map<string, Project>,
  jobLookup: Map<string, Job>,
): string {
  if (rule.scopeType === "project") {
    const project = rule.projectID
      ? projectLookup.get(rule.projectID)
      : undefined;
    return project
      ? `All jobs in ${project.name}`
      : "All jobs in selected project";
  }

  const job = rule.jobID ? jobLookup.get(rule.jobID) : undefined;
  if (!job) {
    return "One specific job";
  }
  const project = projectLookup.get(job.project_id);
  return project ? `${project.name} / ${job.name}` : job.name;
}

export function findProjectIDForRule(
  rule: GroupedSubscriptionRule,
  jobLookup: Map<string, Job>,
): string {
  if (rule.scopeType === "project") {
    return rule.projectID ?? "";
  }
  if (!rule.jobID) {
    return "";
  }
  const job = jobLookup.get(rule.jobID);
  return job ? job.project_id : "";
}

export function draftToIdentity(draft: RuleDraft): RuleIdentity {
  return {
    targetID: draft.targetID.trim(),
    scopeType: draft.scopeType as NotificationScopeType,
    projectID:
      draft.scopeType === "project" ? draft.projectID.trim() : undefined,
    jobID: draft.scopeType === "job" ? draft.jobID.trim() : undefined,
  };
}

function desiredKeyFromRow(subscription: NotificationSubscription): string {
  if (subscription.project_id) {
    return desiredKeyFromIdentityAndEvent(
      {
        targetID: subscription.target_id,
        scopeType: "project",
        projectID: subscription.project_id,
      },
      subscription.event_type,
    );
  }

  return desiredKeyFromIdentityAndEvent(
    {
      targetID: subscription.target_id,
      scopeType: "job",
      jobID: subscription.job_id ?? "",
    },
    subscription.event_type,
  );
}

function desiredKeyFromIdentityAndEvent(
  identity: RuleIdentity,
  eventType: NotificationEventType,
): string {
  const scopeID =
    identity.scopeType === "project" ? identity.projectID : identity.jobID;
  return `${identity.targetID}|${identity.scopeType}|${scopeID ?? ""}|${eventType}`;
}

function ruleKeyFromRow(subscription: NotificationSubscription): string {
  if (subscription.project_id) {
    return `${subscription.target_id}|project|${subscription.project_id}`;
  }
  return `${subscription.target_id}|job|${subscription.job_id ?? ""}`;
}
