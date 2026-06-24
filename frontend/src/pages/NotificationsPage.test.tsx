import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { APIError } from "../api";
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

function getCreateRuleForm(): HTMLElement {
  const heading = screen.getByRole("heading", {
    name: "Create Subscription Rule",
  });
  const panel = heading.closest("section");
  if (!panel) {
    throw new Error("Create Subscription Rule panel not found");
  }
  return panel;
}

function getRulesSection(): HTMLElement {
  const heading = screen.getByRole("heading", {
    name: "Notification Subscription Rules",
  });
  const section = heading.closest("section");
  if (!section) {
    throw new Error("Notification Subscription Rules section not found");
  }
  return section;
}

function clickFirstEditInRules(): void {
  const rulesSection = getRulesSection();
  fireEvent.click(
    within(rulesSection).getAllByRole("button", { name: "Edit" })[0],
  );
}

function clickEditForRule(ruleLabel: string): void {
  const rulesSection = getRulesSection();
  const row = within(rulesSection).getByText(ruleLabel).closest("tr");
  if (!row) {
    throw new Error(`Rule row not found for ${ruleLabel}`);
  }
  fireEvent.click(within(row).getByRole("button", { name: "Edit" }));
}

function clickDeleteForRule(ruleLabel: string): void {
  const rulesSection = getRulesSection();
  const row = within(rulesSection).getByText(ruleLabel).closest("tr");
  if (!row) {
    throw new Error(`Rule row not found for ${ruleLabel}`);
  }
  fireEvent.click(within(row).getByRole("button", { name: "Delete" }));
}

function getEditRulePanel(): HTMLElement {
  const heading = screen.getByRole("heading", { name: "Edit Rule" });
  const container = heading.closest("div");
  if (!container) {
    throw new Error("Edit Rule panel not found");
  }
  return container;
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
        name: "Engineering alerts",
        address: "eng-alerts@example.com",
        webhook_configured: false,
        enabled: true,
        created_at: "2026-06-01T00:00:00Z",
        updated_at: "2026-06-01T00:00:00Z",
      },
      {
        id: "target-2",
        type: "slack_webhook",
        name: "#coyote-ci",
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
      {
        id: "subscription-2",
        target_id: "target-1",
        project_id: "project-1",
        job_id: null,
        event_type: "build_succeeded",
        enabled: true,
        created_at: "2026-06-01T01:00:00Z",
        updated_at: "2026-06-01T01:00:00Z",
      },
      {
        id: "subscription-3",
        target_id: "target-2",
        project_id: null,
        job_id: "job-2",
        event_type: "build_failed",
        enabled: false,
        created_at: "2026-06-01T02:00:00Z",
        updated_at: "2026-06-01T02:00:00Z",
      },
    ]);

    mockedListProjects.mockResolvedValue([
      {
        id: "project-1",
        name: "Coyote CI",
        slug: "coyote-ci",
        created_at: "2026-06-01T00:00:00Z",
        updated_at: "2026-06-01T00:00:00Z",
      },
      {
        id: "project-2",
        name: "Website",
        slug: "website",
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
      {
        id: "job-2",
        project_id: "project-2",
        name: "frontend-ci",
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
      address: "ops@example.com",
      webhook_configured: false,
      enabled: true,
      created_at: "2026-06-02T00:00:00Z",
      updated_at: "2026-06-02T00:00:00Z",
    });

    mockedUpdateNotificationTarget.mockResolvedValue({
      id: "target-1",
      type: "email",
      name: "Engineering alerts",
      address: "eng-alerts@example.com",
      webhook_configured: false,
      enabled: false,
      created_at: "2026-06-01T00:00:00Z",
      updated_at: "2026-06-02T00:00:00Z",
    });

    mockedCreateNotificationSubscription.mockResolvedValue({
      id: "subscription-created",
      target_id: "target-1",
      project_id: "project-1",
      job_id: null,
      event_type: "build_failed",
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

  it("shows target options with channel labels", async () => {
    renderPage();

    await waitFor(() => {
      const form = getCreateRuleForm();
      const select = within(form).getByLabelText(
        "1. Target",
      ) as HTMLSelectElement;
      const labels = Array.from(select.options).map((option) => option.text);
      expect(labels).toContain("Email · Engineering alerts");
      expect(labels).toContain("Slack · #coyote-ci");
    });
  });

  it("defaults build failed selected only", async () => {
    renderPage();

    await waitFor(() => {
      expect(screen.getByLabelText("Build failed")).toBeTruthy();
    });

    const failed = screen.getByLabelText("Build failed") as HTMLInputElement;
    const succeeded = screen.getByLabelText(
      "Build succeeded",
    ) as HTMLInputElement;

    expect(failed.checked).toBe(true);
    expect(succeeded.checked).toBe(false);
  });

  it("validates required fields for creating an email target", async () => {
    renderPage();

    fireEvent.click(
      screen.getByRole("button", { name: "Create Email Target" }),
    );

    await waitFor(() => {
      expect(screen.getByText("Target name is required.")).toBeTruthy();
    });

    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Eng Alerts" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Create Email Target" }),
    );

    await waitFor(() => {
      expect(screen.getByText("Email address is required.")).toBeTruthy();
    });

    fireEvent.change(screen.getByLabelText("Email Address"), {
      target: { value: "invalid-email" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Create Email Target" }),
    );

    await waitFor(() => {
      expect(screen.getByText("Email address must be valid.")).toBeTruthy();
    });
  });

  it("creates an email target and resets the create-target form", async () => {
    renderPage();

    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Eng Alerts" },
    });
    fireEvent.change(screen.getByLabelText("Email Address"), {
      target: { value: "eng-alerts@example.com" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Create Email Target" }),
    );

    await waitFor(() => {
      expect(mockedCreateNotificationTarget).toHaveBeenCalledWith({
        type: "email",
        name: "Eng Alerts",
        address: "eng-alerts@example.com",
        enabled: true,
      });
    });

    await waitFor(() => {
      expect((screen.getByLabelText("Name") as HTMLInputElement).value).toBe(
        "",
      );
      expect(
        (screen.getByLabelText("Email Address") as HTMLInputElement).value,
      ).toBe("");
    });
  });

  it("validates and creates a slack webhook target", async () => {
    renderPage();

    fireEvent.change(screen.getByLabelText("Target Type"), {
      target: { value: "slack_webhook" },
    });
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "#coyote-ci" },
    });
    fireEvent.change(screen.getByLabelText("Webhook URL"), {
      target: { value: "http://hooks.slack.com/services/a/b/c" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Create Slack Webhook" }),
    );

    await waitFor(() => {
      expect(
        screen.getByText("Webhook URL must be an HTTPS URL."),
      ).toBeTruthy();
    });

    fireEvent.change(screen.getByLabelText("Webhook URL"), {
      target: { value: "https://hooks.slack.com/services/a/b/c" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Create Slack Webhook" }),
    );

    await waitFor(() => {
      expect(mockedCreateNotificationTarget).toHaveBeenCalledWith({
        type: "slack_webhook",
        name: "#coyote-ci",
        webhook_url: "https://hooks.slack.com/services/a/b/c",
        enabled: true,
      });
    });
  });

  it("surfaces create-target API errors", async () => {
    mockedCreateNotificationTarget.mockRejectedValueOnce(
      new Error("create target failed"),
    );

    renderPage();

    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Eng Alerts" },
    });
    fireEvent.change(screen.getByLabelText("Email Address"), {
      target: { value: "eng-alerts@example.com" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Create Email Target" }),
    );

    await waitFor(() => {
      expect(
        screen.getByText(/Failed to create notification target/),
      ).toBeTruthy();
    });
  });

  it("surfaces update-target API errors", async () => {
    mockedUpdateNotificationTarget.mockRejectedValueOnce(
      new Error("update target failed"),
    );

    renderPage();

    await waitFor(() => {
      expect(
        screen.getByRole("button", { name: "Disable Engineering alerts" }),
      ).toBeTruthy();
    });

    fireEvent.click(
      screen.getByRole("button", { name: "Disable Engineering alerts" }),
    );

    await waitFor(() => {
      expect(
        screen.getByText(/Failed to update notification target/),
      ).toBeTruthy();
    });
  });

  it("creates one subscription row for one selected event", async () => {
    renderPage();

    await waitFor(() => {
      const form = getCreateRuleForm();
      const select = within(form).getByLabelText(
        "1. Target",
      ) as HTMLSelectElement;
      expect(Array.from(select.options).length).toBeGreaterThan(1);
    });

    fireEvent.change(within(getCreateRuleForm()).getByLabelText("1. Target"), {
      target: { value: "target-1" },
    });
    fireEvent.change(within(getCreateRuleForm()).getByLabelText("3. Scope"), {
      target: { value: "project" },
    });
    fireEvent.change(within(getCreateRuleForm()).getByLabelText("4. Project"), {
      target: { value: "project-1" },
    });

    const succeeded =
      within(getCreateRuleForm()).getByLabelText("Build succeeded");
    const failed = within(getCreateRuleForm()).getByLabelText("Build failed");
    fireEvent.click(failed);
    fireEvent.click(succeeded);

    fireEvent.click(
      within(getCreateRuleForm()).getByRole("button", { name: "Create Rule" }),
    );

    await waitFor(() => {
      expect(mockedCreateNotificationSubscription).toHaveBeenCalledTimes(1);
    });

    expect(mockedCreateNotificationSubscription).toHaveBeenCalledWith({
      target_id: "target-1",
      event_type: "build_succeeded",
      project_id: "project-1",
      job_id: undefined,
      enabled: true,
    });
  });

  it("creates two rows when both events selected", async () => {
    renderPage();

    await waitFor(() => {
      const form = getCreateRuleForm();
      const select = within(form).getByLabelText(
        "1. Target",
      ) as HTMLSelectElement;
      expect(Array.from(select.options).length).toBeGreaterThan(1);
    });

    fireEvent.change(within(getCreateRuleForm()).getByLabelText("1. Target"), {
      target: { value: "target-1" },
    });
    fireEvent.change(within(getCreateRuleForm()).getByLabelText("3. Scope"), {
      target: { value: "project" },
    });
    fireEvent.change(within(getCreateRuleForm()).getByLabelText("4. Project"), {
      target: { value: "project-1" },
    });
    fireEvent.click(
      within(getCreateRuleForm()).getByLabelText("Build succeeded"),
    );
    fireEvent.click(
      within(getCreateRuleForm()).getByRole("button", { name: "Create Rule" }),
    );

    await waitFor(() => {
      expect(mockedCreateNotificationSubscription).toHaveBeenCalledTimes(2);
    });

    expect(
      mockedCreateNotificationSubscription.mock.calls.map((call) => call[0]),
    ).toEqual(
      expect.arrayContaining([
        {
          target_id: "target-1",
          event_type: "build_failed",
          project_id: "project-1",
          job_id: undefined,
          enabled: true,
        },
        {
          target_id: "target-1",
          event_type: "build_succeeded",
          project_id: "project-1",
          job_id: undefined,
          enabled: true,
        },
      ]),
    );
  });

  it("project scope hides job selector", async () => {
    renderPage();

    await waitFor(() => {
      expect(screen.getByLabelText("3. Scope")).toBeTruthy();
    });

    fireEvent.change(screen.getByLabelText("3. Scope"), {
      target: { value: "project" },
    });

    expect(screen.queryByLabelText("Job")).toBeNull();
  });

  it("job scope filters jobs by selected project", async () => {
    renderPage();

    await waitFor(() => {
      expect(screen.getByLabelText("3. Scope")).toBeTruthy();
    });

    fireEvent.change(screen.getByLabelText("3. Scope"), {
      target: { value: "job" },
    });
    fireEvent.change(screen.getByLabelText("4. Project"), {
      target: { value: "project-1" },
    });

    const jobSelect = screen.getByLabelText("Job") as HTMLSelectElement;
    const labels = Array.from(jobSelect.options).map((option) => option.text);

    expect(labels).toContain("backend-ci");
    expect(labels).not.toContain("frontend-ci");
  });

  it("groups success and failure rows into one displayed rule", async () => {
    renderPage();

    const rulesSection = getRulesSection();
    await waitFor(() => {
      expect(
        within(rulesSection).getByText("Email · Engineering alerts"),
      ).toBeTruthy();
    });

    const rulesTable = within(rulesSection).getByRole("table");
    expect(within(rulesTable).getAllByRole("row").length).toBeGreaterThan(1);

    expect(
      within(rulesSection).getByText("Email · Engineering alerts"),
    ).toBeTruthy();
    expect(
      within(rulesSection).getByText("All jobs in Coyote CI"),
    ).toBeTruthy();
    expect(
      within(rulesSection).getAllByText("Build failed").length,
    ).toBeGreaterThan(0);
    expect(
      within(rulesSection).getAllByText("Build succeeded").length,
    ).toBeGreaterThan(0);
  });

  it("shows project-scope fallback when project lookup is missing", async () => {
    mockedListNotificationSubscriptions.mockResolvedValue([
      {
        id: "subscription-missing-project",
        target_id: "target-1",
        project_id: "project-missing",
        job_id: null,
        event_type: "build_failed",
        enabled: true,
        created_at: "2026-06-01T00:00:00Z",
        updated_at: "2026-06-01T00:00:00Z",
      },
    ]);

    renderPage();

    await waitFor(() => {
      expect(
        within(getRulesSection()).getByText("All jobs in selected project"),
      ).toBeTruthy();
    });
  });

  it("shows job-scope fallback when job lookup is missing", async () => {
    mockedListNotificationSubscriptions.mockResolvedValue([
      {
        id: "subscription-missing-job",
        target_id: "target-2",
        project_id: null,
        job_id: "job-missing",
        event_type: "build_failed",
        enabled: false,
        created_at: "2026-06-01T00:00:00Z",
        updated_at: "2026-06-01T00:00:00Z",
      },
    ]);

    renderPage();

    await waitFor(() => {
      expect(
        within(getRulesSection()).getByText("One specific job"),
      ).toBeTruthy();
    });
  });

  it("shows job name when job exists but its project lookup is missing", async () => {
    mockedListNotificationSubscriptions.mockResolvedValue([
      {
        id: "subscription-job-only",
        target_id: "target-2",
        project_id: null,
        job_id: "job-ghost",
        event_type: "build_failed",
        enabled: false,
        created_at: "2026-06-01T00:00:00Z",
        updated_at: "2026-06-01T00:00:00Z",
      },
    ]);
    mockedListProjects.mockResolvedValue([
      {
        id: "project-1",
        name: "Coyote CI",
        slug: "coyote-ci",
        created_at: "2026-06-01T00:00:00Z",
        updated_at: "2026-06-01T00:00:00Z",
      },
    ]);
    mockedListJobs.mockResolvedValue([
      {
        id: "job-ghost",
        project_id: "project-ghost",
        name: "nightly-release",
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

    renderPage();

    await waitFor(() => {
      expect(
        within(getRulesSection()).getByText("nightly-release"),
      ).toBeTruthy();
    });
  });

  it("keeps project unset when editing a job rule whose job is missing", async () => {
    mockedListNotificationSubscriptions.mockResolvedValue([
      {
        id: "subscription-edit-missing-job",
        target_id: "target-2",
        project_id: null,
        job_id: "job-missing",
        event_type: "build_failed",
        enabled: true,
        created_at: "2026-06-01T00:00:00Z",
        updated_at: "2026-06-01T00:00:00Z",
      },
    ]);

    renderPage();

    await waitFor(() => {
      expect(
        within(getRulesSection()).getByText("Slack · #coyote-ci"),
      ).toBeTruthy();
    });

    clickEditForRule("Slack · #coyote-ci");

    const projectSelect = within(getEditRulePanel()).getByLabelText(
      "Project",
    ) as HTMLSelectElement;
    expect(projectSelect.value).toBe("");
  });

  it("does not duplicate event badges when duplicate rows exist", async () => {
    mockedListNotificationSubscriptions.mockResolvedValue([
      {
        id: "subscription-dup-1",
        target_id: "target-1",
        project_id: "project-1",
        job_id: null,
        event_type: "build_failed",
        enabled: true,
        created_at: "2026-06-01T00:00:00Z",
        updated_at: "2026-06-01T00:00:00Z",
      },
      {
        id: "subscription-dup-2",
        target_id: "target-1",
        project_id: "project-1",
        job_id: null,
        event_type: "build_failed",
        enabled: true,
        created_at: "2026-06-01T00:00:01Z",
        updated_at: "2026-06-01T00:00:01Z",
      },
    ]);

    renderPage();

    const rulesSection = getRulesSection();
    await waitFor(() => {
      expect(
        within(rulesSection).getByText("Email · Engineering alerts"),
      ).toBeTruthy();
    });

    expect(within(rulesSection).getAllByText("Build failed").length).toBe(1);
  });

  it("editing adds newly selected event", async () => {
    mockedListNotificationSubscriptions.mockResolvedValue([
      {
        id: "subscription-one",
        target_id: "target-1",
        project_id: "project-1",
        job_id: null,
        event_type: "build_failed",
        enabled: true,
        created_at: "2026-06-01T00:00:00Z",
        updated_at: "2026-06-01T00:00:00Z",
      },
    ]);

    renderPage();

    await waitFor(() => {
      expect(
        within(getRulesSection()).getByText("Email · Engineering alerts"),
      ).toBeTruthy();
    });

    clickEditForRule("Email · Engineering alerts");
    fireEvent.click(
      within(getEditRulePanel()).getByLabelText("Build succeeded"),
    );
    fireEvent.click(screen.getByRole("button", { name: "Save Rule" }));

    await waitFor(() => {
      expect(mockedCreateNotificationSubscription).toHaveBeenCalledWith({
        target_id: "target-1",
        event_type: "build_succeeded",
        project_id: "project-1",
        job_id: undefined,
        enabled: true,
      });
    });
  });

  it("editing removes unselected event", async () => {
    renderPage();

    await waitFor(() => {
      expect(
        within(getRulesSection()).getByText("Email · Engineering alerts"),
      ).toBeTruthy();
    });

    clickEditForRule("Email · Engineering alerts");
    fireEvent.click(
      within(getEditRulePanel()).getByLabelText("Build succeeded"),
    );
    fireEvent.click(screen.getByRole("button", { name: "Save Rule" }));

    await waitFor(() => {
      expect(mockedDeleteNotificationSubscription).toHaveBeenCalledWith(
        "subscription-2",
      );
    });
  });

  it("editing changes scope using create-first then delete", async () => {
    renderPage();

    await waitFor(() => {
      expect(
        within(getRulesSection()).getByText("Email · Engineering alerts"),
      ).toBeTruthy();
    });

    clickEditForRule("Email · Engineering alerts");
    fireEvent.change(within(getEditRulePanel()).getByLabelText("Scope"), {
      target: { value: "job" },
    });
    fireEvent.change(within(getEditRulePanel()).getByLabelText("Project"), {
      target: { value: "project-1" },
    });
    fireEvent.change(within(getEditRulePanel()).getByLabelText("Job"), {
      target: { value: "job-1" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save Rule" }));

    await waitFor(() => {
      expect(mockedCreateNotificationSubscription).toHaveBeenCalled();
      expect(mockedDeleteNotificationSubscription).toHaveBeenCalled();
    });

    expect(
      mockedCreateNotificationSubscription.mock.invocationCallOrder[0],
    ).toBeLessThan(
      mockedDeleteNotificationSubscription.mock.invocationCallOrder[0],
    );
  });

  it("does not delete old rows when scope-replacement create fails", async () => {
    mockedCreateNotificationSubscription.mockRejectedValueOnce(
      new Error("create failed"),
    );

    renderPage();

    await waitFor(() => {
      expect(
        within(getRulesSection()).getByText("Email · Engineering alerts"),
      ).toBeTruthy();
    });

    clickEditForRule("Email · Engineering alerts");
    fireEvent.change(within(getEditRulePanel()).getByLabelText("Scope"), {
      target: { value: "job" },
    });
    fireEvent.change(within(getEditRulePanel()).getByLabelText("Project"), {
      target: { value: "project-1" },
    });
    fireEvent.change(within(getEditRulePanel()).getByLabelText("Job"), {
      target: { value: "job-1" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save Rule" }));

    await waitFor(() => {
      expect(mockedCreateNotificationSubscription).toHaveBeenCalled();
    });

    expect(mockedDeleteNotificationSubscription).not.toHaveBeenCalled();
    expect(mockedUpdateNotificationSubscription).not.toHaveBeenCalled();
    expect(
      screen.getByText(
        /Skipped update and delete phases to preserve existing subscriptions/,
      ),
    ).toBeTruthy();
  });

  it("does not delete existing rows when adding an event fails", async () => {
    mockedListNotificationSubscriptions.mockResolvedValue([
      {
        id: "subscription-one",
        target_id: "target-1",
        project_id: "project-1",
        job_id: null,
        event_type: "build_failed",
        enabled: true,
        created_at: "2026-06-01T00:00:00Z",
        updated_at: "2026-06-01T00:00:00Z",
      },
    ]);
    mockedCreateNotificationSubscription.mockRejectedValueOnce(
      new Error("create failed"),
    );

    renderPage();

    await waitFor(() => {
      expect(
        within(getRulesSection()).getByText("Email · Engineering alerts"),
      ).toBeTruthy();
    });

    clickEditForRule("Email · Engineering alerts");
    fireEvent.click(
      within(getEditRulePanel()).getByLabelText("Build succeeded"),
    );
    fireEvent.click(screen.getByRole("button", { name: "Save Rule" }));

    await waitFor(() => {
      expect(mockedCreateNotificationSubscription).toHaveBeenCalledWith({
        target_id: "target-1",
        event_type: "build_succeeded",
        project_id: "project-1",
        job_id: undefined,
        enabled: true,
      });
    });

    expect(mockedDeleteNotificationSubscription).not.toHaveBeenCalled();
    expect(mockedUpdateNotificationSubscription).not.toHaveBeenCalled();
    expect(
      screen.getByText(
        /Skipped update and delete phases to preserve existing subscriptions/,
      ),
    ).toBeTruthy();
  });

  it("does not delete obsolete rows when kept-row update fails", async () => {
    mockedUpdateNotificationSubscription.mockRejectedValueOnce(
      new Error("update failed"),
    );

    renderPage();

    await waitFor(() => {
      expect(
        within(getRulesSection()).getByText("Email · Engineering alerts"),
      ).toBeTruthy();
    });

    clickEditForRule("Email · Engineering alerts");
    fireEvent.click(
      within(getEditRulePanel()).getByLabelText("Build succeeded"),
    );
    fireEvent.click(within(getEditRulePanel()).getByLabelText("Enabled"));
    fireEvent.click(screen.getByRole("button", { name: "Save Rule" }));

    await waitFor(() => {
      expect(mockedUpdateNotificationSubscription).toHaveBeenCalled();
    });

    expect(mockedDeleteNotificationSubscription).not.toHaveBeenCalled();
    expect(
      screen.getByText(
        /Skipped delete phase to preserve existing subscriptions/,
      ),
    ).toBeTruthy();
  });

  it("allows delete phase after successful create and update phases", async () => {
    mockedListNotificationSubscriptions.mockResolvedValue([
      {
        id: "subscription-keep",
        target_id: "target-1",
        project_id: "project-1",
        job_id: null,
        event_type: "build_failed",
        enabled: true,
        created_at: "2026-06-01T00:00:00Z",
        updated_at: "2026-06-01T00:00:00Z",
      },
      {
        id: "subscription-duplicate",
        target_id: "target-1",
        project_id: "project-1",
        job_id: null,
        event_type: "build_failed",
        enabled: true,
        created_at: "2026-06-01T00:00:01Z",
        updated_at: "2026-06-01T00:00:01Z",
      },
    ]);

    renderPage();

    await waitFor(() => {
      expect(
        within(getRulesSection()).getByText("Email · Engineering alerts"),
      ).toBeTruthy();
    });

    clickEditForRule("Email · Engineering alerts");
    fireEvent.click(
      within(getEditRulePanel()).getByLabelText("Build succeeded"),
    );
    fireEvent.click(within(getEditRulePanel()).getByLabelText("Enabled"));
    fireEvent.click(screen.getByRole("button", { name: "Save Rule" }));

    await waitFor(() => {
      expect(mockedCreateNotificationSubscription).toHaveBeenCalledWith({
        target_id: "target-1",
        event_type: "build_succeeded",
        project_id: "project-1",
        job_id: undefined,
        enabled: false,
      });
      expect(mockedUpdateNotificationSubscription).toHaveBeenCalledWith(
        "subscription-keep",
        { enabled: false },
      );
      expect(mockedDeleteNotificationSubscription).toHaveBeenCalledWith(
        "subscription-duplicate",
      );
    });
  });

  it("treats 409 create as satisfied and continues to later phases", async () => {
    mockedListNotificationSubscriptions.mockResolvedValue([
      {
        id: "subscription-keep",
        target_id: "target-1",
        project_id: "project-1",
        job_id: null,
        event_type: "build_failed",
        enabled: true,
        created_at: "2026-06-01T00:00:00Z",
        updated_at: "2026-06-01T00:00:00Z",
      },
      {
        id: "subscription-duplicate",
        target_id: "target-1",
        project_id: "project-1",
        job_id: null,
        event_type: "build_failed",
        enabled: true,
        created_at: "2026-06-01T00:00:01Z",
        updated_at: "2026-06-01T00:00:01Z",
      },
    ]);
    mockedCreateNotificationSubscription.mockRejectedValueOnce(
      new APIError(409, "already exists", "conflict"),
    );

    renderPage();

    await waitFor(() => {
      expect(
        within(getRulesSection()).getByText("Email · Engineering alerts"),
      ).toBeTruthy();
    });

    clickEditForRule("Email · Engineering alerts");
    fireEvent.click(
      within(getEditRulePanel()).getByLabelText("Build succeeded"),
    );
    fireEvent.click(within(getEditRulePanel()).getByLabelText("Enabled"));
    fireEvent.click(screen.getByRole("button", { name: "Save Rule" }));

    await waitFor(() => {
      expect(mockedUpdateNotificationSubscription).toHaveBeenCalled();
      expect(mockedDeleteNotificationSubscription).toHaveBeenCalledWith(
        "subscription-duplicate",
      );
    });
  });

  it("shows mixed-state warning when editing a mixed enabled rule", async () => {
    mockedListNotificationSubscriptions.mockResolvedValue([
      {
        id: "subscription-mixed-enabled",
        target_id: "target-1",
        project_id: "project-1",
        job_id: null,
        event_type: "build_failed",
        enabled: true,
        created_at: "2026-06-01T00:00:00Z",
        updated_at: "2026-06-01T00:00:00Z",
      },
      {
        id: "subscription-mixed-disabled",
        target_id: "target-1",
        project_id: "project-1",
        job_id: null,
        event_type: "build_succeeded",
        enabled: false,
        created_at: "2026-06-01T00:00:01Z",
        updated_at: "2026-06-01T00:00:01Z",
      },
    ]);

    renderPage();

    await waitFor(() => {
      expect(
        within(getRulesSection()).getByText("Email · Engineering alerts"),
      ).toBeTruthy();
    });

    clickFirstEditInRules();

    await waitFor(() => {
      expect(
        screen.getByText(
          "This rule currently has mixed enabled states across events. Saving will apply the selected enabled state to all events in this rule.",
        ),
      ).toBeTruthy();
    });
  });

  it("grouped deletion deletes all represented rows", async () => {
    renderPage();

    await waitFor(() => {
      const rulesSection = getRulesSection();
      expect(
        within(rulesSection).getAllByRole("button", { name: "Delete" }).length,
      ).toBeGreaterThan(0);
    });

    const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(true);
    clickDeleteForRule("Email · Engineering alerts");

    await waitFor(() => {
      expect(mockedDeleteNotificationSubscription).toHaveBeenCalledWith(
        "subscription-1",
      );
      expect(mockedDeleteNotificationSubscription).toHaveBeenCalledWith(
        "subscription-2",
      );
    });

    confirmSpy.mockRestore();
  });

  it("surfaces partial create failures", async () => {
    mockedCreateNotificationSubscription
      .mockResolvedValueOnce({
        id: "ok",
        target_id: "target-1",
        project_id: "project-1",
        job_id: null,
        event_type: "build_failed",
        enabled: true,
        created_at: "2026-06-02T00:00:00Z",
        updated_at: "2026-06-02T00:00:00Z",
      })
      .mockRejectedValueOnce(new Error("network failed"));

    renderPage();

    await waitFor(() => {
      const form = getCreateRuleForm();
      const select = within(form).getByLabelText(
        "1. Target",
      ) as HTMLSelectElement;
      expect(Array.from(select.options).length).toBeGreaterThan(1);
    });

    fireEvent.change(within(getCreateRuleForm()).getByLabelText("1. Target"), {
      target: { value: "target-1" },
    });
    fireEvent.change(within(getCreateRuleForm()).getByLabelText("3. Scope"), {
      target: { value: "project" },
    });
    fireEvent.change(within(getCreateRuleForm()).getByLabelText("4. Project"), {
      target: { value: "project-1" },
    });
    fireEvent.click(
      within(getCreateRuleForm()).getByLabelText("Build succeeded"),
    );
    fireEvent.click(
      within(getCreateRuleForm()).getByRole("button", { name: "Create Rule" }),
    );

    await waitFor(() => {
      expect(
        screen.getByText(/Created 1 event subscription\(s\); 1 failed/),
      ).toBeTruthy();
    });
  });

  it("treats create conflicts as already satisfied", async () => {
    mockedCreateNotificationSubscription.mockRejectedValue(
      new APIError(409, "already exists", "conflict"),
    );

    renderPage();

    await waitFor(() => {
      const form = getCreateRuleForm();
      const select = within(form).getByLabelText(
        "1. Target",
      ) as HTMLSelectElement;
      expect(Array.from(select.options).length).toBeGreaterThan(1);
    });

    fireEvent.change(within(getCreateRuleForm()).getByLabelText("1. Target"), {
      target: { value: "target-1" },
    });
    fireEvent.change(within(getCreateRuleForm()).getByLabelText("3. Scope"), {
      target: { value: "project" },
    });
    fireEvent.change(within(getCreateRuleForm()).getByLabelText("4. Project"), {
      target: { value: "project-1" },
    });
    fireEvent.click(
      within(getCreateRuleForm()).getByRole("button", { name: "Create Rule" }),
    );

    await waitFor(() => {
      expect(screen.getByText(/already existed/)).toBeTruthy();
    });
  });

  it("shows no-target empty state and guides to create target", async () => {
    mockedListNotificationTargets.mockResolvedValue([]);
    renderPage();

    await waitFor(() => {
      expect(
        screen.getByText(
          "No targets exist yet. Create a target before creating a subscription rule.",
        ),
      ).toBeTruthy();
    });
  });

  it("shows no-subscriptions empty state", async () => {
    mockedListNotificationSubscriptions.mockResolvedValue([]);
    renderPage();

    await waitFor(() => {
      expect(
        screen.getByText(
          "No notification subscriptions have been created yet.",
        ),
      ).toBeTruthy();
    });
  });

  it("keeps existing target management intact", async () => {
    renderPage();

    await waitFor(() => {
      expect(
        screen.getByRole("button", { name: "Disable Engineering alerts" }),
      ).toBeTruthy();
    });

    fireEvent.click(
      screen.getByRole("button", { name: "Disable Engineering alerts" }),
    );

    await waitFor(() => {
      expect(mockedUpdateNotificationTarget).toHaveBeenCalledWith("target-1", {
        enabled: false,
      });
    });
  });
});
