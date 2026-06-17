import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { NotificationsPage } from "./NotificationsPage";
import {
  createNotificationSubscription,
  createNotificationTarget,
  deleteNotificationSubscription,
  listJobs,
  listNotificationSubscriptions,
  listNotificationTargets,
  listProjects,
  updateNotificationSubscription,
  updateNotificationTarget,
} from "../api";

vi.mock("../api", async () => {
  const actual = await vi.importActual<typeof import("../api")>("../api");
  return {
    ...actual,
    createNotificationSubscription: vi.fn(),
    createNotificationTarget: vi.fn(),
    deleteNotificationSubscription: vi.fn(),
    listJobs: vi.fn(),
    listNotificationSubscriptions: vi.fn(),
    listNotificationTargets: vi.fn(),
    listProjects: vi.fn(),
    updateNotificationSubscription: vi.fn(),
    updateNotificationTarget: vi.fn(),
  };
});

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <NotificationsPage />
    </QueryClientProvider>,
  );
}

describe("NotificationsPage", () => {
  const mockedCreateNotificationSubscription = vi.mocked(
    createNotificationSubscription,
  );
  const mockedCreateNotificationTarget = vi.mocked(createNotificationTarget);
  const mockedDeleteNotificationSubscription = vi.mocked(
    deleteNotificationSubscription,
  );
  const mockedListJobs = vi.mocked(listJobs);
  const mockedListNotificationSubscriptions = vi.mocked(
    listNotificationSubscriptions,
  );
  const mockedListNotificationTargets = vi.mocked(listNotificationTargets);
  const mockedListProjects = vi.mocked(listProjects);
  const mockedUpdateNotificationSubscription = vi.mocked(
    updateNotificationSubscription,
  );
  const mockedUpdateNotificationTarget = vi.mocked(updateNotificationTarget);

  beforeEach(() => {
    vi.clearAllMocks();

    mockedListNotificationTargets.mockResolvedValue([
      {
        id: "target-1",
        type: "email",
        name: "Dev Mailbox",
        address: "dev@localhost",
        webhook_configured: false,
        enabled: true,
        created_at: "2026-06-01T00:00:00Z",
        updated_at: "2026-06-01T00:00:00Z",
      },
      {
        id: "target-2",
        type: "slack_webhook",
        name: "Build Alerts",
        webhook_configured: true,
        enabled: true,
        created_at: "2026-06-01T00:00:00Z",
        updated_at: "2026-06-01T00:00:00Z",
      },
    ]);
    mockedListNotificationSubscriptions.mockResolvedValue([
      {
        id: "subscription-1",
        target_id: "target-1",
        project_id: "project-1",
        job_id: null,
        event_type: "build_failed",
        enabled: true,
        created_at: "2026-06-01T00:00:00Z",
        updated_at: "2026-06-01T00:00:00Z",
      },
    ]);
    mockedListProjects.mockResolvedValue([
      {
        id: "project-1",
        name: "Backend",
        slug: "backend",
        created_at: "2026-06-01T00:00:00Z",
        updated_at: "2026-06-01T00:00:00Z",
      },
    ]);
    mockedListJobs.mockResolvedValue([
      {
        id: "job-1",
        project_id: "project-1",
        name: "backend-ci",
        priority: 5,
        repository_url: "https://example.com/repo.git",
        default_ref: "main",
        push_enabled: true,
        pipeline_yaml: "steps: []",
        enabled: true,
        created_at: "2026-06-01T00:00:00Z",
        updated_at: "2026-06-01T00:00:00Z",
      },
    ]);
    mockedCreateNotificationTarget.mockResolvedValue({
      id: "target-3",
      type: "email",
      name: "Ops",
      address: "ops@localhost",
      webhook_configured: false,
      enabled: true,
      created_at: "2026-06-02T00:00:00Z",
      updated_at: "2026-06-02T00:00:00Z",
    });
    mockedUpdateNotificationTarget.mockResolvedValue({
      id: "target-1",
      type: "email",
      name: "Dev Mailbox",
      address: "dev@localhost",
      webhook_configured: false,
      enabled: false,
      created_at: "2026-06-01T00:00:00Z",
      updated_at: "2026-06-02T00:00:00Z",
    });
    mockedCreateNotificationSubscription.mockResolvedValue({
      id: "subscription-2",
      target_id: "target-1",
      project_id: "project-1",
      job_id: null,
      event_type: "build_succeeded",
      enabled: true,
      created_at: "2026-06-02T00:00:00Z",
      updated_at: "2026-06-02T00:00:00Z",
    });
    mockedUpdateNotificationSubscription.mockResolvedValue({
      id: "subscription-1",
      target_id: "target-1",
      project_id: "project-1",
      job_id: null,
      event_type: "build_failed",
      enabled: false,
      created_at: "2026-06-01T00:00:00Z",
      updated_at: "2026-06-02T00:00:00Z",
    });
    mockedDeleteNotificationSubscription.mockResolvedValue();
  });

  it("renders notification targets and subscriptions", async () => {
    renderPage();

    await waitFor(() => {
      expect(screen.getByText("Notifications")).toBeTruthy();
      expect(screen.getByDisplayValue("Dev Mailbox")).toBeTruthy();
      expect(screen.getByText("Webhook configured")).toBeTruthy();
      expect(screen.getAllByText("build_failed").length).toBeGreaterThan(0);
      expect(screen.getByText("project: Backend")).toBeTruthy();
    });
  });

  it("creates an email target", async () => {
    renderPage();

    await waitFor(() => {
      expect(screen.getByDisplayValue("Dev Mailbox")).toBeTruthy();
    });

    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Ops Mailbox" },
    });
    fireEvent.change(screen.getByLabelText("Email Address"), {
      target: { value: "ops@localhost" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Create Email Target" }),
    );

    await waitFor(() => {
      expect(mockedCreateNotificationTarget).toHaveBeenCalledWith({
        type: "email",
        name: "Ops Mailbox",
        address: "ops@localhost",
        enabled: true,
      });
    });
  });

  it("toggles a target enabled state", async () => {
    renderPage();

    await waitFor(() => {
      expect(
        screen.getByRole("button", { name: "Disable Dev Mailbox" }),
      ).toBeTruthy();
    });

    fireEvent.click(
      screen.getByRole("button", { name: "Disable Dev Mailbox" }),
    );

    await waitFor(() => {
      expect(mockedUpdateNotificationTarget).toHaveBeenCalledWith("target-1", {
        enabled: false,
      });
    });
  });

  it("creates a project-level subscription", async () => {
    renderPage();

    await waitFor(() => {
      expect(
        (screen.getByLabelText("Target") as HTMLSelectElement).options,
      ).toHaveLength(3);
    });

    fireEvent.change(screen.getByLabelText("Target"), {
      target: { value: "target-1" },
    });
    fireEvent.change(screen.getByLabelText("Event Type"), {
      target: { value: "build_succeeded" },
    });
    fireEvent.change(screen.getByLabelText("Scope Type"), {
      target: { value: "project" },
    });
    fireEvent.change(screen.getByLabelText("Project"), {
      target: { value: "project-1" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Create Subscription" }),
    );

    await waitFor(() => {
      expect(mockedCreateNotificationSubscription).toHaveBeenCalledWith({
        target_id: "target-1",
        event_type: "build_succeeded",
        project_id: "project-1",
        enabled: true,
      });
    });
  });

  it("creates a job-level subscription", async () => {
    renderPage();

    await waitFor(() => {
      expect(
        (screen.getByLabelText("Target") as HTMLSelectElement).options,
      ).toHaveLength(3);
    });

    fireEvent.change(screen.getByLabelText("Target"), {
      target: { value: "target-1" },
    });
    fireEvent.change(screen.getByLabelText("Event Type"), {
      target: { value: "build_failed" },
    });
    fireEvent.change(screen.getByLabelText("Scope Type"), {
      target: { value: "job" },
    });
    fireEvent.change(screen.getByLabelText("Job"), {
      target: { value: "job-1" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Create Subscription" }),
    );

    await waitFor(() => {
      expect(mockedCreateNotificationSubscription).toHaveBeenCalledWith({
        target_id: "target-1",
        event_type: "build_failed",
        job_id: "job-1",
        enabled: true,
      });
    });
  });

  it("deletes a subscription", async () => {
    renderPage();

    await waitFor(() => {
      expect(
        screen.getByRole("button", { name: "Delete subscription" }),
      ).toBeTruthy();
    });

    fireEvent.click(
      screen.getByRole("button", { name: "Delete subscription" }),
    );

    await waitFor(() => {
      expect(mockedDeleteNotificationSubscription).toHaveBeenCalledWith(
        "subscription-1",
      );
    });
  });

  it("validates invalid email and missing subscription fields", async () => {
    renderPage();

    await waitFor(() => {
      expect(
        screen.getByRole("button", { name: "Create Email Target" }),
      ).toBeTruthy();
    });

    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Broken" },
    });
    fireEvent.change(screen.getByLabelText("Email Address"), {
      target: { value: "not-an-email" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Create Email Target" }),
    );

    await waitFor(() => {
      expect(screen.getByText("Email address must be valid.")).toBeTruthy();
    });

    fireEvent.click(
      screen.getByRole("button", { name: "Create Subscription" }),
    );

    await waitFor(() => {
      expect(screen.getByText("Notification target is required.")).toBeTruthy();
    });

    fireEvent.change(screen.getByLabelText("Target"), {
      target: { value: "target-1" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Create Subscription" }),
    );

    await waitFor(() => {
      expect(screen.getByText("Event type is required.")).toBeTruthy();
    });

    fireEvent.change(screen.getByLabelText("Event Type"), {
      target: { value: "build_succeeded" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Create Subscription" }),
    );

    await waitFor(() => {
      expect(screen.getByText("Scope is required.")).toBeTruthy();
    });
  });

  it("creates a slack webhook target", async () => {
    mockedCreateNotificationTarget.mockResolvedValue({
      id: "target-3",
      type: "slack_webhook",
      name: "Build Alerts",
      webhook_configured: true,
      enabled: true,
      created_at: "2026-06-02T00:00:00Z",
      updated_at: "2026-06-02T00:00:00Z",
    });

    renderPage();

    await waitFor(() => {
      expect(screen.getByLabelText("Target Type")).toBeTruthy();
    });

    fireEvent.change(screen.getByLabelText("Target Type"), {
      target: { value: "slack_webhook" },
    });
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Build Alerts" },
    });
    fireEvent.change(screen.getByLabelText("Webhook URL"), {
      target: { value: "https://hooks.slack.com/services/T/B/X" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Create Slack Webhook" }),
    );

    await waitFor(() => {
      expect(mockedCreateNotificationTarget).toHaveBeenCalledWith({
        type: "slack_webhook",
        name: "Build Alerts",
        webhook_url: "https://hooks.slack.com/services/T/B/X",
        enabled: true,
      });
    });
  });

  it("edits a slack target without re-entering its url", async () => {
    mockedUpdateNotificationTarget.mockResolvedValue({
      id: "target-2",
      type: "slack_webhook",
      name: "Release Alerts",
      webhook_configured: true,
      enabled: true,
      created_at: "2026-06-01T00:00:00Z",
      updated_at: "2026-06-02T00:00:00Z",
    });

    renderPage();

    await waitFor(() => {
      expect(screen.getByDisplayValue("Build Alerts")).toBeTruthy();
    });

    fireEvent.change(screen.getByLabelText("Name for Build Alerts"), {
      target: { value: "Release Alerts" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save Build Alerts" }));

    await waitFor(() => {
      expect(mockedUpdateNotificationTarget).toHaveBeenCalledWith("target-2", {
        name: "Release Alerts",
      });
    });
  });

  it("replaces a slack webhook url without displaying the stored secret", async () => {
    mockedUpdateNotificationTarget.mockResolvedValue({
      id: "target-2",
      type: "slack_webhook",
      name: "Build Alerts",
      webhook_configured: true,
      enabled: true,
      created_at: "2026-06-01T00:00:00Z",
      updated_at: "2026-06-02T00:00:00Z",
    });

    renderPage();

    await waitFor(() => {
      expect(screen.getByText("Webhook configured")).toBeTruthy();
    });

    expect(
      screen.queryByDisplayValue("https://hooks.slack.com/services/T/B/X"),
    ).toBeNull();

    fireEvent.change(screen.getByLabelText("Webhook URL for Build Alerts"), {
      target: { value: "https://hooks.slack.com/services/T/B/Y" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save Build Alerts" }));

    await waitFor(() => {
      expect(mockedUpdateNotificationTarget).toHaveBeenCalledWith("target-2", {
        name: "Build Alerts",
        webhook_url: "https://hooks.slack.com/services/T/B/Y",
      });
    });
  });

  it("validates slack webhook urls", async () => {
    renderPage();

    await waitFor(() => {
      expect(screen.getByLabelText("Target Type")).toBeTruthy();
    });

    fireEvent.change(screen.getByLabelText("Target Type"), {
      target: { value: "slack_webhook" },
    });
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Broken Slack" },
    });
    fireEvent.change(screen.getByLabelText("Webhook URL"), {
      target: { value: "http://hooks.slack.com/services/T/B/X" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Create Slack Webhook" }),
    );

    await waitFor(() => {
      expect(
        screen.getByText("Webhook URL must be an HTTPS URL."),
      ).toBeTruthy();
    });
  });

  it("shows target types in subscription selection", async () => {
    renderPage();

    await waitFor(() => {
      expect(
        (screen.getByLabelText("Target") as HTMLSelectElement).options,
      ).toHaveLength(3);
    });

    const select = screen.getByLabelText("Target") as HTMLSelectElement;
    const labels = Array.from(select.options).map((option) => option.text);

    expect(labels).toContain("Dev Mailbox (Email: dev@localhost)");
    expect(labels).toContain(
      "Build Alerts (Slack webhook, Webhook configured)",
    );
  });
});
