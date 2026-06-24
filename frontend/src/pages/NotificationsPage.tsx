import { useMemo, useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  createNotificationSubscription,
  createNotificationTarget,
  deleteNotificationSubscription,
  formatAPIErrorMessage,
  isAPIErrorStatus,
  listJobs,
  listNotificationSubscriptions,
  listNotificationTargets,
  listProjects,
  updateNotificationSubscription,
  updateNotificationTarget,
} from "../api";
import type { Job } from "../types/job";
import type { Project } from "../types/project";
import type {
  NotificationEventType,
  NotificationSubscription,
  NotificationTarget,
  NotificationTargetType,
} from "../types/notification";
import { formatTime } from "../utils/time";

const EVENT_OPTIONS: NotificationEventType[] = [
  "build_failed",
  "build_succeeded",
];

const EMAIL_PATTERN = /^[^\s@]+@[^\s@]+$/;
const SLACK_WEBHOOK_PATTERN = /^https:\/\/.+/i;

type NotificationScopeType = "project" | "job";

type EventSelection = Record<NotificationEventType, boolean>;

type RuleEnabledState = "enabled" | "disabled" | "mixed";

interface GroupedSubscriptionRule {
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

interface RuleDraft {
  targetID: string;
  scopeType: NotificationScopeType | "";
  projectID: string;
  jobID: string;
  events: EventSelection;
  enabled: boolean;
}

interface ReconcilePlan {
  createEvents: NotificationEventType[];
  updateEnabledRows: NotificationSubscription[];
  deleteRows: NotificationSubscription[];
}

interface OperationResult {
  completed: number;
  alreadySatisfied: number;
  failures: string[];
}

interface RuleIdentity {
  targetID: string;
  scopeType: NotificationScopeType;
  projectID?: string;
  jobID?: string;
}

const DEFAULT_EVENT_SELECTION: EventSelection = {
  build_failed: true,
  build_succeeded: false,
};

export function NotificationsPage() {
  const queryClient = useQueryClient();
  const [targetType, setTargetType] = useState<NotificationTargetType>("email");
  const [targetName, setTargetName] = useState("");
  const [targetAddress, setTargetAddress] = useState("");
  const [targetEnabled, setTargetEnabled] = useState(true);

  const [createDraft, setCreateDraft] = useState<RuleDraft>({
    targetID: "",
    scopeType: "",
    projectID: "",
    jobID: "",
    events: { ...DEFAULT_EVENT_SELECTION },
    enabled: true,
  });

  const [editingRuleKey, setEditingRuleKey] = useState<string | null>(null);
  const [editDraft, setEditDraft] = useState<RuleDraft>({
    targetID: "",
    scopeType: "",
    projectID: "",
    jobID: "",
    events: { ...DEFAULT_EVENT_SELECTION },
    enabled: true,
  });

  const [targetErrorMessage, setTargetErrorMessage] = useState<string | null>(
    null,
  );
  const [subscriptionErrorMessage, setSubscriptionErrorMessage] = useState<
    string | null
  >(null);
  const [actionErrorMessage, setActionErrorMessage] = useState<string | null>(
    null,
  );
  const [actionNoticeMessage, setActionNoticeMessage] = useState<string | null>(
    null,
  );
  const [subscriptionActionPending, setSubscriptionActionPending] =
    useState(false);

  const {
    data: targets = [],
    isLoading: targetsLoading,
    error: targetsError,
  } = useQuery({
    queryKey: ["notification-targets"],
    queryFn: listNotificationTargets,
  });

  const {
    data: subscriptions = [],
    isLoading: subscriptionsLoading,
    error: subscriptionsError,
  } = useQuery({
    queryKey: ["notification-subscriptions"],
    queryFn: () => listNotificationSubscriptions(),
  });

  const { data: projects = [] } = useQuery({
    queryKey: ["projects"],
    queryFn: () => listProjects(),
  });

  const { data: jobs = [] } = useQuery({
    queryKey: ["jobs"],
    queryFn: () => listJobs(),
  });

  const targetLookup = useMemo(
    () => new Map(targets.map((target) => [target.id, target])),
    [targets],
  );
  const projectLookup = useMemo(
    () => new Map(projects.map((project) => [project.id, project])),
    [projects],
  );
  const jobLookup = useMemo(
    () => new Map(jobs.map((job) => [job.id, job])),
    [jobs],
  );

  const groupedRules = useMemo(
    () => groupSubscriptions(subscriptions),
    [subscriptions],
  );
  const editingRule = useMemo(
    () =>
      editingRuleKey
        ? (groupedRules.find((candidate) => candidate.key === editingRuleKey) ??
          null)
        : null,
    [groupedRules, editingRuleKey],
  );

  const selectedCreateProjectJobs = useMemo(
    () =>
      jobs.filter(
        (job) =>
          createDraft.projectID && job.project_id === createDraft.projectID,
      ),
    [jobs, createDraft.projectID],
  );

  const selectedEditProjectJobs = useMemo(
    () =>
      jobs.filter(
        (job) => editDraft.projectID && job.project_id === editDraft.projectID,
      ),
    [jobs, editDraft.projectID],
  );

  const createTargetMutation = useMutation({
    mutationFn: (input: Parameters<typeof createNotificationTarget>[0]) =>
      createNotificationTarget(input),
    onMutate: () => {
      setTargetErrorMessage(null);
      setActionErrorMessage(null);
      setActionNoticeMessage(null);
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: ["notification-targets"],
      });
      setTargetType("email");
      setTargetName("");
      setTargetAddress("");
      setTargetEnabled(true);
    },
    onError: (mutationError) => {
      setActionErrorMessage(
        formatAPIErrorMessage(
          mutationError,
          "You do not have permission to manage notification settings.",
          "Failed to create notification target",
        ),
      );
    },
  });

  const updateTargetMutation = useMutation({
    mutationFn: ({
      id,
      input,
    }: {
      id: string;
      input: Parameters<typeof updateNotificationTarget>[1];
    }) => updateNotificationTarget(id, input),
    onMutate: () => {
      setActionErrorMessage(null);
      setActionNoticeMessage(null);
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: ["notification-targets"],
      });
    },
    onError: (mutationError) => {
      setActionErrorMessage(
        formatAPIErrorMessage(
          mutationError,
          "You do not have permission to manage notification settings.",
          "Failed to update notification target",
        ),
      );
    },
  });

  const resetCreateDraft = () => {
    setCreateDraft({
      targetID: "",
      scopeType: "",
      projectID: "",
      jobID: "",
      events: { ...DEFAULT_EVENT_SELECTION },
      enabled: true,
    });
  };

  const refreshSubscriptions = async () => {
    await queryClient.invalidateQueries({
      queryKey: ["notification-subscriptions"],
    });
  };

  const onCreateTarget = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const trimmedName = targetName.trim();
    const trimmedAddress = targetAddress.trim();

    if (!trimmedName) {
      setTargetErrorMessage("Target name is required.");
      return;
    }

    if (targetType === "email") {
      if (!trimmedAddress) {
        setTargetErrorMessage("Email address is required.");
        return;
      }
      if (!EMAIL_PATTERN.test(trimmedAddress)) {
        setTargetErrorMessage("Email address must be valid.");
        return;
      }

      createTargetMutation.mutate({
        type: targetType,
        name: trimmedName,
        address: trimmedAddress,
        enabled: targetEnabled,
      });
      return;
    }

    if (!trimmedAddress) {
      setTargetErrorMessage("Webhook URL is required.");
      return;
    }
    if (!SLACK_WEBHOOK_PATTERN.test(trimmedAddress)) {
      setTargetErrorMessage("Webhook URL must be an HTTPS URL.");
      return;
    }

    createTargetMutation.mutate({
      type: targetType,
      name: trimmedName,
      webhook_url: trimmedAddress,
      enabled: targetEnabled,
    });
  };

  const onCreateRule = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (subscriptionActionPending) {
      return;
    }

    const validationError = validateRuleDraft(createDraft);
    if (validationError) {
      setSubscriptionErrorMessage(validationError);
      return;
    }

    const selectedEvents = selectedEventsFromDraft(createDraft);
    if (selectedEvents.length === 0) {
      setSubscriptionErrorMessage("Select at least one event.");
      return;
    }

    setSubscriptionErrorMessage(null);
    setActionErrorMessage(null);
    setActionNoticeMessage(null);
    setSubscriptionActionPending(true);

    const createResult = await createRowsForDraft(createDraft, selectedEvents);

    await refreshSubscriptions();
    setSubscriptionActionPending(false);

    if (createResult.failures.length > 0) {
      setActionErrorMessage(
        `Created ${createResult.completed} event subscription(s); ${createResult.failures.length} failed: ${createResult.failures.join("; ")}`,
      );
      return;
    }

    resetCreateDraft();
    const conflictNote =
      createResult.alreadySatisfied > 0
        ? ` (${createResult.alreadySatisfied} already existed)`
        : "";
    setActionNoticeMessage(
      `Saved notification rule with ${selectedEvents.length} event subscription(s)${conflictNote}.`,
    );
  };

  const onStartEditRule = (rule: GroupedSubscriptionRule) => {
    const projectID =
      rule.scopeType === "project"
        ? (rule.projectID ?? "")
        : findProjectIDForRule(rule, jobLookup);
    const nextDraft: RuleDraft = {
      targetID: rule.targetID,
      scopeType: rule.scopeType,
      projectID,
      jobID: rule.jobID ?? "",
      events: {
        build_failed: rule.eventTypes.includes("build_failed"),
        build_succeeded: rule.eventTypes.includes("build_succeeded"),
      },
      enabled: rule.enabledState === "enabled",
    };

    if (!nextDraft.events.build_failed && !nextDraft.events.build_succeeded) {
      nextDraft.events.build_failed = true;
    }
    if (nextDraft.scopeType === "project") {
      nextDraft.jobID = "";
    }

    setEditingRuleKey(rule.key);
    setEditDraft(nextDraft);
    setActionErrorMessage(null);
    setActionNoticeMessage(null);
  };

  const onCancelEditRule = () => {
    setEditingRuleKey(null);
    setEditDraft({
      targetID: "",
      scopeType: "",
      projectID: "",
      jobID: "",
      events: { ...DEFAULT_EVENT_SELECTION },
      enabled: true,
    });
  };

  const onSaveEditedRule = async () => {
    if (!editingRuleKey || subscriptionActionPending) {
      return;
    }
    const rule = editingRule;
    if (!rule) {
      setActionErrorMessage(
        "The selected rule no longer exists. Refresh and try again.",
      );
      setEditingRuleKey(null);
      return;
    }

    const validationError = validateRuleDraft(editDraft);
    if (validationError) {
      setActionErrorMessage(validationError);
      return;
    }

    const selectedEvents = selectedEventsFromDraft(editDraft);
    if (selectedEvents.length === 0) {
      setActionErrorMessage("Select at least one event.");
      return;
    }

    setActionErrorMessage(null);
    setActionNoticeMessage(null);
    setSubscriptionActionPending(true);

    const plan = buildReconcilePlan(rule, editDraft);
    const createResult = await createRowsForDraft(editDraft, plan.createEvents);
    if (createResult.failures.length > 0) {
      await refreshSubscriptions();
      setSubscriptionActionPending(false);
      setActionErrorMessage(
        `Updated rule with partial failures: ${createResult.failures.join("; ")}. Skipped update and delete phases to preserve existing subscriptions.`,
      );
      return;
    }

    const updateResult = await updateEnabledRows(
      plan.updateEnabledRows,
      editDraft.enabled,
    );
    if (updateResult.failures.length > 0) {
      await refreshSubscriptions();
      setSubscriptionActionPending(false);
      setActionErrorMessage(
        `Updated rule with partial failures: ${updateResult.failures.join("; ")}. Skipped delete phase to preserve existing subscriptions.`,
      );
      return;
    }

    const deleteResult = await deleteRows(plan.deleteRows);

    await refreshSubscriptions();
    setSubscriptionActionPending(false);

    if (deleteResult.failures.length > 0) {
      setActionErrorMessage(
        `Updated rule with partial failures: ${deleteResult.failures.join("; ")}`,
      );
      return;
    }

    onCancelEditRule();
    const alreadySatisfied = createResult.alreadySatisfied;
    const satisfiedSuffix =
      alreadySatisfied > 0 ? ` (${alreadySatisfied} already existed)` : "";
    setActionNoticeMessage(`Updated notification rule.${satisfiedSuffix}`);
  };

  const onDeleteRule = async (rule: GroupedSubscriptionRule) => {
    if (subscriptionActionPending) {
      return;
    }

    const target = targetLookup.get(rule.targetID);
    const confirmationTarget = target
      ? `${formatTargetTypeLabel(target.type)} · ${target.name}`
      : "Unknown target";
    const scope = describeScope(rule, projectLookup, jobLookup);
    const confirmed = window.confirm(
      `Delete notification rule for ${confirmationTarget} (${scope})? This deletes only subscriptions, not the target.`,
    );
    if (!confirmed) {
      return;
    }

    setActionErrorMessage(null);
    setActionNoticeMessage(null);
    setSubscriptionActionPending(true);

    const deleteResult = await deleteRows(rule.subscriptions);
    await refreshSubscriptions();
    setSubscriptionActionPending(false);

    if (deleteResult.failures.length > 0) {
      setActionErrorMessage(
        `Deleted ${deleteResult.completed} subscription row(s); ${deleteResult.failures.length} failed: ${deleteResult.failures.join("; ")}`,
      );
      return;
    }

    if (editingRuleKey === rule.key) {
      onCancelEditRule();
    }
    setActionNoticeMessage("Deleted notification rule.");
  };

  const noTargets = !targetsLoading && targets.length === 0;

  return (
    <>
      <div className="page-header-row">
        <div>
          <h2>Notifications</h2>
          <p className="subtle-text">
            Targets define where notifications go. Subscriptions define which
            build events are sent to each target.
          </p>
        </div>
      </div>

      <div className="settings-grid">
        <section className="settings-panel">
          <h3>Create Target</h3>
          <form className="job-form" onSubmit={onCreateTarget} noValidate>
            <label htmlFor="notification-target-type">Target Type</label>
            <select
              id="notification-target-type"
              value={targetType}
              onChange={(event) => {
                setTargetType(event.target.value as NotificationTargetType);
                setTargetAddress("");
                setTargetErrorMessage(null);
              }}
              disabled={createTargetMutation.isPending}
            >
              <option value="email">Email</option>
              <option value="slack_webhook">Slack webhook</option>
            </select>

            <label htmlFor="notification-target-name">Name</label>
            <input
              id="notification-target-name"
              value={targetName}
              onChange={(event) => {
                setTargetName(event.target.value);
                setTargetErrorMessage(null);
              }}
              disabled={createTargetMutation.isPending}
              placeholder={
                targetType === "email" ? "Engineering alerts" : "#coyote-ci"
              }
            />

            <label htmlFor="notification-target-address">
              {targetType === "email" ? "Email Address" : "Webhook URL"}
            </label>
            <input
              id="notification-target-address"
              type={targetType === "email" ? "email" : "url"}
              value={targetAddress}
              onChange={(event) => {
                setTargetAddress(event.target.value);
                setTargetErrorMessage(null);
              }}
              disabled={createTargetMutation.isPending}
              placeholder={
                targetType === "email"
                  ? "alerts@company.com"
                  : "https://hooks.slack.com/services/..."
              }
            />

            <label
              className="checkbox-label"
              htmlFor="notification-target-enabled"
            >
              <input
                id="notification-target-enabled"
                type="checkbox"
                checked={targetEnabled}
                onChange={(event) => setTargetEnabled(event.target.checked)}
                disabled={createTargetMutation.isPending}
              />
              Enabled
            </label>

            <div className="job-form-actions">
              <button type="submit" disabled={createTargetMutation.isPending}>
                {createTargetMutation.isPending
                  ? "Creating…"
                  : targetType === "email"
                    ? "Create Email Target"
                    : "Create Slack Webhook"}
              </button>
            </div>
          </form>
          {targetErrorMessage && (
            <p className="error-text">{targetErrorMessage}</p>
          )}
        </section>

        <section className="settings-panel">
          <h3>Create Subscription Rule</h3>
          <form className="job-form" onSubmit={onCreateRule}>
            <label htmlFor="notification-subscription-target">1. Target</label>
            <select
              id="notification-subscription-target"
              value={createDraft.targetID}
              onChange={(event) =>
                setCreateDraft((current) => ({
                  ...current,
                  targetID: event.target.value,
                }))
              }
              disabled={
                subscriptionActionPending || targetsLoading || noTargets
              }
            >
              <option value="">Select a target</option>
              {targets.map((target) => (
                <option key={target.id} value={target.id}>
                  {formatTargetSelectorLabel(target)}
                </option>
              ))}
            </select>

            <fieldset style={{ border: "none", margin: 0, padding: 0 }}>
              <legend style={{ fontWeight: 600, marginBottom: 6 }}>
                2. Events
              </legend>
              {EVENT_OPTIONS.map((eventType) => (
                <label
                  key={`create-event-${eventType}`}
                  className="checkbox-label"
                  htmlFor={`create-event-${eventType}`}
                >
                  <input
                    id={`create-event-${eventType}`}
                    type="checkbox"
                    checked={createDraft.events[eventType]}
                    onChange={(event) =>
                      setCreateDraft((current) => ({
                        ...current,
                        events: {
                          ...current.events,
                          [eventType]: event.target.checked,
                        },
                      }))
                    }
                    disabled={subscriptionActionPending || noTargets}
                  />
                  {formatEventLabel(eventType)}
                </label>
              ))}
            </fieldset>

            <label htmlFor="notification-subscription-scope">3. Scope</label>
            <select
              id="notification-subscription-scope"
              value={createDraft.scopeType}
              onChange={(event) => {
                const nextScope = event.target.value as
                  | NotificationScopeType
                  | "";
                setCreateDraft((current) => ({
                  ...current,
                  scopeType: nextScope,
                  projectID: "",
                  jobID: "",
                }));
              }}
              disabled={subscriptionActionPending || noTargets}
            >
              <option value="">Select scope</option>
              <option value="project">All jobs in a project</option>
              <option value="job">One specific job</option>
            </select>

            {createDraft.scopeType === "project" && (
              <p className="subtle-text" style={{ margin: 0 }}>
                Project scope applies to current and future jobs in the selected
                project.
              </p>
            )}

            {createDraft.scopeType && (
              <>
                <label htmlFor="notification-subscription-project">
                  4. Project
                </label>
                <select
                  id="notification-subscription-project"
                  value={createDraft.projectID}
                  onChange={(event) => {
                    const nextProjectID = event.target.value;
                    setCreateDraft((current) => ({
                      ...current,
                      projectID: nextProjectID,
                      jobID:
                        current.scopeType === "job" && current.jobID
                          ? jobs.some(
                              (job) =>
                                job.id === current.jobID &&
                                job.project_id === nextProjectID,
                            )
                            ? current.jobID
                            : ""
                          : "",
                    }));
                  }}
                  disabled={subscriptionActionPending || noTargets}
                >
                  <option value="">Select a project</option>
                  {projects.map((project) => (
                    <option key={project.id} value={project.id}>
                      {project.name} ({project.slug})
                    </option>
                  ))}
                </select>
              </>
            )}

            {createDraft.scopeType === "job" && (
              <>
                <label htmlFor="notification-subscription-job">Job</label>
                <select
                  id="notification-subscription-job"
                  value={createDraft.jobID}
                  onChange={(event) =>
                    setCreateDraft((current) => ({
                      ...current,
                      jobID: event.target.value,
                    }))
                  }
                  disabled={
                    subscriptionActionPending ||
                    noTargets ||
                    !createDraft.projectID
                  }
                >
                  <option value="">
                    {createDraft.projectID
                      ? "Select a job"
                      : "Select a project first"}
                  </option>
                  {selectedCreateProjectJobs.map((job) => (
                    <option key={job.id} value={job.id}>
                      {job.name}
                    </option>
                  ))}
                </select>
                {createDraft.projectID &&
                  selectedCreateProjectJobs.length === 0 && (
                    <p className="subtle-text" style={{ margin: 0 }}>
                      No jobs are available in this project.
                    </p>
                  )}
              </>
            )}

            <label
              className="checkbox-label"
              htmlFor="notification-subscription-enabled"
            >
              <input
                id="notification-subscription-enabled"
                type="checkbox"
                checked={createDraft.enabled}
                onChange={(event) =>
                  setCreateDraft((current) => ({
                    ...current,
                    enabled: event.target.checked,
                  }))
                }
                disabled={subscriptionActionPending || noTargets}
              />
              Enabled
            </label>

            <div className="job-form-actions">
              <button
                type="submit"
                disabled={subscriptionActionPending || noTargets}
              >
                {subscriptionActionPending ? "Saving…" : "Create Rule"}
              </button>
            </div>
          </form>
          {noTargets && (
            <p className="subtle-text">
              No targets exist yet. Create a target before creating a
              subscription rule.
            </p>
          )}
          {subscriptionErrorMessage && (
            <p className="error-text">{subscriptionErrorMessage}</p>
          )}
        </section>
      </div>

      {actionNoticeMessage && (
        <p className="subtle-text">{actionNoticeMessage}</p>
      )}
      {actionErrorMessage && <p className="error-text">{actionErrorMessage}</p>}
      {targetsError && (
        <p className="error-text">
          {formatAPIErrorMessage(
            targetsError,
            "You do not have permission to manage notification settings.",
            "Failed to load notification targets",
          )}
        </p>
      )}
      {subscriptionsError && (
        <p className="error-text">
          {formatAPIErrorMessage(
            subscriptionsError,
            "You do not have permission to manage notification settings.",
            "Failed to load notification subscriptions",
          )}
        </p>
      )}

      <section className="settings-panel" style={{ marginTop: 16 }}>
        <h3>Notification Targets</h3>
        {targetsLoading && <p>Loading notification targets…</p>}
        {!targetsLoading && targets.length === 0 && (
          <p className="subtle-text">
            No notification targets have been created yet.
          </p>
        )}
        {targets.length > 0 && (
          <table className="table">
            <thead>
              <tr>
                <th>Name</th>
                <th>Destination</th>
                <th>Type</th>
                <th>Status</th>
                <th>Updated</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              {targets.map((target) => (
                <NotificationTargetRow
                  key={`${target.id}-${target.updated_at}`}
                  target={target}
                  disabled={
                    updateTargetMutation.isPending || subscriptionActionPending
                  }
                  onSave={(input) =>
                    updateTargetMutation.mutate({ id: target.id, input })
                  }
                  onToggle={() =>
                    updateTargetMutation.mutate({
                      id: target.id,
                      input: { enabled: !target.enabled },
                    })
                  }
                />
              ))}
            </tbody>
          </table>
        )}
      </section>

      <section className="settings-panel" style={{ marginTop: 16 }}>
        <h3>Notification Subscription Rules</h3>
        {subscriptionsLoading && <p>Loading notification subscriptions…</p>}
        {!subscriptionsLoading && groupedRules.length === 0 && (
          <p className="subtle-text">
            No notification subscriptions have been created yet.
          </p>
        )}
        {groupedRules.length > 0 && (
          <table className="table">
            <thead>
              <tr>
                <th>Rule</th>
                <th>Scope</th>
                <th>Events</th>
                <th>Status</th>
                <th>Updated</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              {groupedRules.map((rule) => {
                const target = targetLookup.get(rule.targetID);
                const isEditing = editingRuleKey === rule.key;
                return (
                  <tr key={rule.key}>
                    <td>
                      <strong>
                        {target
                          ? `${formatTargetTypeLabel(target.type)} · ${target.name}`
                          : "Unknown target"}
                      </strong>
                    </td>
                    <td>{describeScope(rule, projectLookup, jobLookup)}</td>
                    <td>
                      <div
                        style={{ display: "flex", gap: 6, flexWrap: "wrap" }}
                      >
                        {rule.eventTypes.map((eventType) => (
                          <span
                            key={`${rule.key}-${eventType}`}
                            className="trigger-badge"
                          >
                            {formatEventLabel(eventType)}
                          </span>
                        ))}
                      </div>
                    </td>
                    <td>{formatRuleEnabledState(rule.enabledState)}</td>
                    <td>{formatTime(rule.updatedAt)}</td>
                    <td>
                      <div className="table-actions">
                        <button
                          type="button"
                          className="table-action-button"
                          onClick={() => onStartEditRule(rule)}
                          disabled={subscriptionActionPending}
                        >
                          Edit
                        </button>
                        <button
                          type="button"
                          className="table-action-button"
                          onClick={() => {
                            void onDeleteRule(rule);
                          }}
                          disabled={subscriptionActionPending}
                        >
                          Delete
                        </button>
                        {isEditing && (
                          <span
                            className="subtle-text"
                            style={{ fontSize: "0.8rem" }}
                          >
                            Editing below
                          </span>
                        )}
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        )}

        {editingRuleKey && (
          <div
            style={{
              marginTop: 14,
              borderTop: "1px solid var(--color-border-muted)",
              paddingTop: 14,
            }}
          >
            <h4 style={{ marginTop: 0, marginBottom: 10 }}>Edit Rule</h4>
            {editingRule?.enabledState === "mixed" && (
              <p className="subtle-text" style={{ marginTop: 0 }}>
                This rule currently has mixed enabled states across events.
                Saving will apply the selected enabled state to all events in
                this rule.
              </p>
            )}
            <form
              className="job-form"
              onSubmit={(event) => {
                event.preventDefault();
                void onSaveEditedRule();
              }}
            >
              <label htmlFor="notification-edit-target">Target</label>
              <select
                id="notification-edit-target"
                value={editDraft.targetID}
                onChange={(event) =>
                  setEditDraft((current) => ({
                    ...current,
                    targetID: event.target.value,
                  }))
                }
                disabled={subscriptionActionPending}
              >
                <option value="">Select a target</option>
                {targets.map((target) => (
                  <option key={`edit-target-${target.id}`} value={target.id}>
                    {formatTargetSelectorLabel(target)}
                  </option>
                ))}
              </select>

              <fieldset style={{ border: "none", margin: 0, padding: 0 }}>
                <legend style={{ fontWeight: 600, marginBottom: 6 }}>
                  Events
                </legend>
                {EVENT_OPTIONS.map((eventType) => (
                  <label
                    key={`edit-event-${eventType}`}
                    className="checkbox-label"
                    htmlFor={`edit-event-${eventType}`}
                  >
                    <input
                      id={`edit-event-${eventType}`}
                      type="checkbox"
                      checked={editDraft.events[eventType]}
                      onChange={(event) =>
                        setEditDraft((current) => ({
                          ...current,
                          events: {
                            ...current.events,
                            [eventType]: event.target.checked,
                          },
                        }))
                      }
                      disabled={subscriptionActionPending}
                    />
                    {formatEventLabel(eventType)}
                  </label>
                ))}
              </fieldset>

              <label htmlFor="notification-edit-scope">Scope</label>
              <select
                id="notification-edit-scope"
                value={editDraft.scopeType}
                onChange={(event) => {
                  const nextScope = event.target.value as NotificationScopeType;
                  setEditDraft((current) => ({
                    ...current,
                    scopeType: nextScope,
                    projectID: "",
                    jobID: "",
                  }));
                }}
                disabled={subscriptionActionPending}
              >
                <option value="project">All jobs in a project</option>
                <option value="job">One specific job</option>
              </select>

              {editDraft.scopeType === "project" && (
                <p className="subtle-text" style={{ margin: 0 }}>
                  Project scope applies to current and future jobs in the
                  selected project.
                </p>
              )}

              <label htmlFor="notification-edit-project">Project</label>
              <select
                id="notification-edit-project"
                value={editDraft.projectID}
                onChange={(event) => {
                  const nextProjectID = event.target.value;
                  setEditDraft((current) => ({
                    ...current,
                    projectID: nextProjectID,
                    jobID:
                      current.scopeType === "job" && current.jobID
                        ? jobs.some(
                            (job) =>
                              job.id === current.jobID &&
                              job.project_id === nextProjectID,
                          )
                          ? current.jobID
                          : ""
                        : "",
                  }));
                }}
                disabled={subscriptionActionPending}
              >
                <option value="">Select a project</option>
                {projects.map((project) => (
                  <option key={`edit-project-${project.id}`} value={project.id}>
                    {project.name} ({project.slug})
                  </option>
                ))}
              </select>

              {editDraft.scopeType === "job" && (
                <>
                  <label htmlFor="notification-edit-job">Job</label>
                  <select
                    id="notification-edit-job"
                    value={editDraft.jobID}
                    onChange={(event) =>
                      setEditDraft((current) => ({
                        ...current,
                        jobID: event.target.value,
                      }))
                    }
                    disabled={subscriptionActionPending || !editDraft.projectID}
                  >
                    <option value="">
                      {editDraft.projectID
                        ? "Select a job"
                        : "Select a project first"}
                    </option>
                    {selectedEditProjectJobs.map((job) => (
                      <option key={`edit-job-${job.id}`} value={job.id}>
                        {job.name}
                      </option>
                    ))}
                  </select>
                  {editDraft.projectID &&
                    selectedEditProjectJobs.length === 0 && (
                      <p className="subtle-text" style={{ margin: 0 }}>
                        No jobs are available in this project.
                      </p>
                    )}
                </>
              )}

              <label
                className="checkbox-label"
                htmlFor="notification-edit-enabled"
              >
                <input
                  id="notification-edit-enabled"
                  type="checkbox"
                  checked={editDraft.enabled}
                  onChange={(event) =>
                    setEditDraft((current) => ({
                      ...current,
                      enabled: event.target.checked,
                    }))
                  }
                  disabled={subscriptionActionPending}
                />
                Enabled
              </label>

              <div className="job-form-actions">
                <button type="submit" disabled={subscriptionActionPending}>
                  {subscriptionActionPending ? "Saving…" : "Save Rule"}
                </button>
                <button
                  type="button"
                  onClick={onCancelEditRule}
                  disabled={subscriptionActionPending}
                >
                  Cancel
                </button>
              </div>
            </form>
          </div>
        )}
      </section>
    </>
  );

  async function createRowsForDraft(
    draft: RuleDraft,
    events: NotificationEventType[],
  ): Promise<OperationResult> {
    const identity = draftToIdentity(draft);
    const failures: string[] = [];
    let completed = 0;
    let alreadySatisfied = 0;

    for (const eventType of events) {
      try {
        await createNotificationSubscription({
          target_id: identity.targetID,
          event_type: eventType,
          project_id:
            identity.scopeType === "project" ? identity.projectID : undefined,
          job_id: identity.scopeType === "job" ? identity.jobID : undefined,
          enabled: draft.enabled,
        });
        completed += 1;
      } catch (error) {
        if (isAPIErrorStatus(error, 409)) {
          alreadySatisfied += 1;
          continue;
        }
        failures.push(
          `${formatEventLabel(eventType)} create failed (${formatAPIErrorMessage(
            error,
            "You do not have permission to manage notification settings.",
            "",
          )})`,
        );
      }
    }

    return { completed, alreadySatisfied, failures };
  }

  async function updateEnabledRows(
    rows: NotificationSubscription[],
    enabled: boolean,
  ): Promise<OperationResult> {
    const failures: string[] = [];
    let completed = 0;

    for (const row of rows) {
      try {
        await updateNotificationSubscription(row.id, { enabled });
        completed += 1;
      } catch (error) {
        failures.push(
          `${formatEventLabel(row.event_type)} update failed (${formatAPIErrorMessage(
            error,
            "You do not have permission to manage notification settings.",
            "",
          )})`,
        );
      }
    }

    return { completed, alreadySatisfied: 0, failures };
  }

  async function deleteRows(
    rows: NotificationSubscription[],
  ): Promise<OperationResult> {
    const failures: string[] = [];
    let completed = 0;

    for (const row of rows) {
      try {
        await deleteNotificationSubscription(row.id);
        completed += 1;
      } catch (error) {
        failures.push(
          `${formatEventLabel(row.event_type)} delete failed (${formatAPIErrorMessage(
            error,
            "You do not have permission to manage notification settings.",
            "",
          )})`,
        );
      }
    }

    return { completed, alreadySatisfied: 0, failures };
  }
}

function NotificationTargetRow({
  target,
  disabled,
  onSave,
  onToggle,
}: {
  target: NotificationTarget;
  disabled: boolean;
  onSave: (input: {
    name: string;
    address?: string;
    webhook_url?: string;
  }) => void;
  onToggle: () => void;
}) {
  const [name, setName] = useState(target.name);
  const [address, setAddress] = useState(target.address ?? "");
  const [webhookURL, setWebhookURL] = useState("");

  return (
    <tr>
      <td>
        <input
          aria-label={`Name for ${target.name}`}
          value={name}
          onChange={(event) => setName(event.target.value)}
          disabled={disabled}
        />
      </td>
      <td>
        {target.type === "email" ? (
          <input
            aria-label={`Address for ${target.name}`}
            value={address}
            onChange={(event) => setAddress(event.target.value)}
            disabled={disabled}
          />
        ) : (
          <div>
            <div>
              {target.webhook_configured
                ? "Webhook configured"
                : "Webhook missing"}
            </div>
            <input
              aria-label={`Webhook URL for ${target.name}`}
              type="url"
              value={webhookURL}
              onChange={(event) => setWebhookURL(event.target.value)}
              disabled={disabled}
              placeholder="Paste replacement webhook URL"
            />
          </div>
        )}
      </td>
      <td>{formatTargetTypeLabel(target.type)}</td>
      <td>{target.enabled ? "Enabled" : "Disabled"}</td>
      <td>{formatTime(target.updated_at)}</td>
      <td>
        <div className="table-actions">
          <button
            type="button"
            className="table-action-button"
            onClick={() => {
              const input =
                target.type === "email"
                  ? { name: name.trim(), address: address.trim() }
                  : {
                      name: name.trim(),
                      ...(webhookURL.trim()
                        ? { webhook_url: webhookURL.trim() }
                        : {}),
                    };
              onSave(input);
            }}
            disabled={disabled}
          >
            Save {target.name}
          </button>
          <button
            type="button"
            className="table-action-button"
            onClick={onToggle}
            disabled={disabled}
          >
            {target.enabled ? "Disable" : "Enable"} {target.name}
          </button>
        </div>
      </td>
    </tr>
  );
}

function groupSubscriptions(
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

function buildReconcilePlan(
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

function selectedEventsFromDraft(draft: RuleDraft): NotificationEventType[] {
  return EVENT_OPTIONS.filter((eventType) => draft.events[eventType]);
}

function validateRuleDraft(draft: RuleDraft): string | null {
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

function draftToIdentity(draft: RuleDraft): RuleIdentity {
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

function formatTargetTypeLabel(type: NotificationTargetType): string {
  return type === "slack_webhook" ? "Slack" : "Email";
}

function formatTargetSelectorLabel(target: NotificationTarget): string {
  return `${formatTargetTypeLabel(target.type)} · ${target.name}`;
}

function formatEventLabel(eventType: NotificationEventType): string {
  return eventType === "build_failed" ? "Build failed" : "Build succeeded";
}

function formatRuleEnabledState(state: RuleEnabledState): string {
  if (state === "mixed") {
    return "Mixed";
  }
  return state === "enabled" ? "Enabled" : "Disabled";
}

function describeScope(
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

function findProjectIDForRule(
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
