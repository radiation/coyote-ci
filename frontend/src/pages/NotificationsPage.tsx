import { useMemo, useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  createNotificationSubscription,
  createNotificationTarget,
  deleteNotificationSubscription,
  formatAPIErrorMessage,
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
} from "../types/notification";
import { formatTime } from "../utils/time";

const EVENT_OPTIONS: NotificationEventType[] = [
  "build_succeeded",
  "build_failed",
];

const EMAIL_PATTERN = /^[^\s@]+@[^\s@]+$/;

type NotificationScopeType = "" | "project" | "job";

export function NotificationsPage() {
  const queryClient = useQueryClient();
  const [targetName, setTargetName] = useState("");
  const [targetAddress, setTargetAddress] = useState("");
  const [targetEnabled, setTargetEnabled] = useState(true);
  const [subscriptionTargetID, setSubscriptionTargetID] = useState("");
  const [subscriptionEventType, setSubscriptionEventType] = useState<
    NotificationEventType | ""
  >("");
  const [subscriptionScopeType, setSubscriptionScopeType] =
    useState<NotificationScopeType>("");
  const [subscriptionProjectID, setSubscriptionProjectID] = useState("");
  const [subscriptionJobID, setSubscriptionJobID] = useState("");
  const [subscriptionEnabled, setSubscriptionEnabled] = useState(true);
  const [targetErrorMessage, setTargetErrorMessage] = useState<string | null>(
    null,
  );
  const [subscriptionErrorMessage, setSubscriptionErrorMessage] = useState<
    string | null
  >(null);
  const [actionErrorMessage, setActionErrorMessage] = useState<string | null>(
    null,
  );

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

  const createTargetMutation = useMutation({
    mutationFn: (input: Parameters<typeof createNotificationTarget>[0]) =>
      createNotificationTarget(input),
    onMutate: () => {
      setTargetErrorMessage(null);
      setActionErrorMessage(null);
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: ["notification-targets"],
      });
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

  const createSubscriptionMutation = useMutation({
    mutationFn: (input: Parameters<typeof createNotificationSubscription>[0]) =>
      createNotificationSubscription(input),
    onMutate: () => {
      setSubscriptionErrorMessage(null);
      setActionErrorMessage(null);
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: ["notification-subscriptions"],
      });
      setSubscriptionTargetID("");
      setSubscriptionEventType("");
      setSubscriptionScopeType("");
      setSubscriptionProjectID("");
      setSubscriptionJobID("");
      setSubscriptionEnabled(true);
    },
    onError: (mutationError) => {
      setActionErrorMessage(
        formatAPIErrorMessage(
          mutationError,
          "You do not have permission to manage notification settings.",
          "Failed to create notification subscription",
        ),
      );
    },
  });

  const updateSubscriptionMutation = useMutation({
    mutationFn: ({
      id,
      input,
    }: {
      id: string;
      input: Parameters<typeof updateNotificationSubscription>[1];
    }) => updateNotificationSubscription(id, input),
    onMutate: () => {
      setActionErrorMessage(null);
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: ["notification-subscriptions"],
      });
    },
    onError: (mutationError) => {
      setActionErrorMessage(
        formatAPIErrorMessage(
          mutationError,
          "You do not have permission to manage notification settings.",
          "Failed to update notification subscription",
        ),
      );
    },
  });

  const deleteSubscriptionMutation = useMutation({
    mutationFn: (id: string) => deleteNotificationSubscription(id),
    onMutate: () => {
      setActionErrorMessage(null);
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: ["notification-subscriptions"],
      });
    },
    onError: (mutationError) => {
      setActionErrorMessage(
        formatAPIErrorMessage(
          mutationError,
          "You do not have permission to manage notification settings.",
          "Failed to delete notification subscription",
        ),
      );
    },
  });

  const onCreateTarget = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const trimmedName = targetName.trim();
    const trimmedAddress = targetAddress.trim();

    if (!trimmedName) {
      setTargetErrorMessage("Target name is required.");
      return;
    }
    if (!trimmedAddress) {
      setTargetErrorMessage("Email address is required.");
      return;
    }
    if (!EMAIL_PATTERN.test(trimmedAddress)) {
      setTargetErrorMessage("Email address must be valid.");
      return;
    }

    createTargetMutation.mutate({
      name: trimmedName,
      address: trimmedAddress,
      enabled: targetEnabled,
    });
  };

  const onCreateSubscription = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();

    if (!subscriptionTargetID.trim()) {
      setSubscriptionErrorMessage("Notification target is required.");
      return;
    }
    if (!subscriptionEventType) {
      setSubscriptionErrorMessage("Event type is required.");
      return;
    }
    if (!subscriptionScopeType) {
      setSubscriptionErrorMessage("Scope is required.");
      return;
    }

    if (subscriptionScopeType === "project") {
      if (!subscriptionProjectID.trim()) {
        setSubscriptionErrorMessage("Project is required for project scope.");
        return;
      }

      createSubscriptionMutation.mutate({
        target_id: subscriptionTargetID,
        event_type: subscriptionEventType,
        project_id: subscriptionProjectID,
        enabled: subscriptionEnabled,
      });
      return;
    }

    if (!subscriptionJobID.trim()) {
      setSubscriptionErrorMessage("Job is required for job scope.");
      return;
    }

    createSubscriptionMutation.mutate({
      target_id: subscriptionTargetID,
      event_type: subscriptionEventType,
      job_id: subscriptionJobID,
      enabled: subscriptionEnabled,
    });
  };

  return (
    <>
      <div className="page-header-row">
        <div>
          <h2>Notifications</h2>
          <p className="subtle-text">
            Manage email targets and build result subscriptions for local and
            admin verification flows.
          </p>
        </div>
      </div>

      <div className="settings-grid">
        <section className="settings-panel">
          <h3>Create Email Target</h3>
          <form className="job-form" onSubmit={onCreateTarget} noValidate>
            <label htmlFor="notification-target-name">Name</label>
            <input
              id="notification-target-name"
              value={targetName}
              onChange={(event) => setTargetName(event.target.value)}
              disabled={createTargetMutation.isPending}
              placeholder="Dev Mailbox"
            />

            <label htmlFor="notification-target-address">Email Address</label>
            <input
              id="notification-target-address"
              type="email"
              value={targetAddress}
              onChange={(event) => setTargetAddress(event.target.value)}
              disabled={createTargetMutation.isPending}
              placeholder="dev@localhost"
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
                  : "Create Email Target"}
              </button>
            </div>
          </form>
          {targetErrorMessage && (
            <p className="error-text">{targetErrorMessage}</p>
          )}
        </section>

        <section className="settings-panel">
          <h3>Create Subscription</h3>
          <form className="job-form" onSubmit={onCreateSubscription}>
            <label htmlFor="notification-subscription-target">Target</label>
            <select
              id="notification-subscription-target"
              value={subscriptionTargetID}
              onChange={(event) => setSubscriptionTargetID(event.target.value)}
              disabled={createSubscriptionMutation.isPending || targetsLoading}
            >
              <option value="">Select a target</option>
              {targets.map((target) => (
                <option key={target.id} value={target.id}>
                  {target.name} ({target.address})
                </option>
              ))}
            </select>

            <label htmlFor="notification-subscription-event">Event Type</label>
            <select
              id="notification-subscription-event"
              value={subscriptionEventType}
              onChange={(event) =>
                setSubscriptionEventType(
                  event.target.value as NotificationEventType | "",
                )
              }
              disabled={createSubscriptionMutation.isPending}
            >
              <option value="">Select an event</option>
              {EVENT_OPTIONS.map((eventType) => (
                <option key={eventType} value={eventType}>
                  {eventType}
                </option>
              ))}
            </select>

            <label htmlFor="notification-subscription-scope">Scope Type</label>
            <select
              id="notification-subscription-scope"
              value={subscriptionScopeType}
              onChange={(event) => {
                const nextScope = event.target.value as NotificationScopeType;
                setSubscriptionScopeType(nextScope);
                setSubscriptionProjectID("");
                setSubscriptionJobID("");
              }}
              disabled={createSubscriptionMutation.isPending}
            >
              <option value="">Select a scope</option>
              <option value="project">project</option>
              <option value="job">job</option>
            </select>

            {subscriptionScopeType === "project" && (
              <>
                <label htmlFor="notification-subscription-project">
                  Project
                </label>
                <select
                  id="notification-subscription-project"
                  value={subscriptionProjectID}
                  onChange={(event) =>
                    setSubscriptionProjectID(event.target.value)
                  }
                  disabled={createSubscriptionMutation.isPending}
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

            {subscriptionScopeType === "job" && (
              <>
                <label htmlFor="notification-subscription-job">Job</label>
                <select
                  id="notification-subscription-job"
                  value={subscriptionJobID}
                  onChange={(event) => setSubscriptionJobID(event.target.value)}
                  disabled={createSubscriptionMutation.isPending}
                >
                  <option value="">Select a job</option>
                  {jobs.map((job) => {
                    const project = projectLookup.get(job.project_id);
                    const projectLabel = project
                      ? `${project.name} / ${job.name}`
                      : job.name;

                    return (
                      <option key={job.id} value={job.id}>
                        {projectLabel}
                      </option>
                    );
                  })}
                </select>
              </>
            )}

            <label
              className="checkbox-label"
              htmlFor="notification-subscription-enabled"
            >
              <input
                id="notification-subscription-enabled"
                type="checkbox"
                checked={subscriptionEnabled}
                onChange={(event) =>
                  setSubscriptionEnabled(event.target.checked)
                }
                disabled={createSubscriptionMutation.isPending}
              />
              Enabled
            </label>

            <div className="job-form-actions">
              <button
                type="submit"
                disabled={
                  createSubscriptionMutation.isPending || targets.length === 0
                }
              >
                {createSubscriptionMutation.isPending
                  ? "Creating…"
                  : "Create Subscription"}
              </button>
            </div>
          </form>
          {targets.length === 0 && !targetsLoading && (
            <p className="subtle-text">
              Create an email target first before adding subscriptions.
            </p>
          )}
          {subscriptionErrorMessage && (
            <p className="error-text">{subscriptionErrorMessage}</p>
          )}
        </section>
      </div>

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
                <th>Address</th>
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
                  disabled={updateTargetMutation.isPending}
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
        <h3>Notification Subscriptions</h3>
        {subscriptionsLoading && <p>Loading notification subscriptions…</p>}
        {!subscriptionsLoading && subscriptions.length === 0 && (
          <p className="subtle-text">
            No notification subscriptions have been created yet.
          </p>
        )}
        {subscriptions.length > 0 && (
          <table className="table">
            <thead>
              <tr>
                <th>Target</th>
                <th>Event</th>
                <th>Scope</th>
                <th>Status</th>
                <th>Updated</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              {subscriptions.map((subscription) => (
                <NotificationSubscriptionRow
                  key={subscription.id}
                  subscription={subscription}
                  targetLookup={targetLookup}
                  projectLookup={projectLookup}
                  jobLookup={jobLookup}
                  disabled={
                    updateSubscriptionMutation.isPending ||
                    deleteSubscriptionMutation.isPending
                  }
                  onToggle={() =>
                    updateSubscriptionMutation.mutate({
                      id: subscription.id,
                      input: { enabled: !subscription.enabled },
                    })
                  }
                  onDelete={() =>
                    deleteSubscriptionMutation.mutate(subscription.id)
                  }
                />
              ))}
            </tbody>
          </table>
        )}
      </section>
    </>
  );
}

function NotificationTargetRow({
  target,
  disabled,
  onSave,
  onToggle,
}: {
  target: NotificationTarget;
  disabled: boolean;
  onSave: (input: { name: string; address: string }) => void;
  onToggle: () => void;
}) {
  const [name, setName] = useState(target.name);
  const [address, setAddress] = useState(target.address);

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
        <input
          aria-label={`Address for ${target.name}`}
          value={address}
          onChange={(event) => setAddress(event.target.value)}
          disabled={disabled}
        />
      </td>
      <td>{target.type}</td>
      <td>{target.enabled ? "Enabled" : "Disabled"}</td>
      <td>{formatTime(target.updated_at)}</td>
      <td>
        <div className="table-actions">
          <button
            type="button"
            className="table-action-button"
            onClick={() =>
              onSave({ name: name.trim(), address: address.trim() })
            }
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

function NotificationSubscriptionRow({
  subscription,
  targetLookup,
  projectLookup,
  jobLookup,
  disabled,
  onToggle,
  onDelete,
}: {
  subscription: NotificationSubscription;
  targetLookup: Map<string, NotificationTarget>;
  projectLookup: Map<string, Project>;
  jobLookup: Map<string, Job>;
  disabled: boolean;
  onToggle: () => void;
  onDelete: () => void;
}) {
  const target = targetLookup.get(subscription.target_id);
  const scopeLabel = formatSubscriptionScope(
    subscription,
    projectLookup,
    jobLookup,
  );

  return (
    <tr>
      <td>
        {target ? `${target.name} (${target.address})` : subscription.target_id}
      </td>
      <td>{subscription.event_type}</td>
      <td>{scopeLabel}</td>
      <td>{subscription.enabled ? "Enabled" : "Disabled"}</td>
      <td>{formatTime(subscription.updated_at)}</td>
      <td>
        <div className="table-actions">
          <button
            type="button"
            className="table-action-button"
            onClick={onToggle}
            disabled={disabled}
          >
            {subscription.enabled ? "Disable" : "Enable"} subscription
          </button>
          <button
            type="button"
            className="table-action-button"
            onClick={onDelete}
            disabled={disabled}
          >
            Delete subscription
          </button>
        </div>
      </td>
    </tr>
  );
}

function formatSubscriptionScope(
  subscription: NotificationSubscription,
  projectLookup: Map<string, Project>,
  jobLookup: Map<string, Job>,
): string {
  if (subscription.project_id) {
    const project = projectLookup.get(subscription.project_id);
    return project
      ? `project: ${project.name}`
      : `project: ${subscription.project_id}`;
  }

  if (subscription.job_id) {
    const job = jobLookup.get(subscription.job_id);
    if (!job) {
      return `job: ${subscription.job_id}`;
    }

    const project = projectLookup.get(job.project_id);
    return project ? `job: ${project.name} / ${job.name}` : `job: ${job.name}`;
  }

  return "—";
}
