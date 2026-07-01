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
import { AuthContext, type AuthContextValue } from "../auth-context";
import {
  createNotificationSubscription,
  createNotificationTarget,
  deleteSlackWorkspaceIntegration,
  deleteNotificationSubscription,
  getNotificationDefaults,
  getSlackWorkspaceIntegration,
  listJobs,
  listNotificationSubscriptions,
  listNotificationTargets,
  listProjects,
  patchSlackWorkspaceIntegration,
  putSlackWorkspaceIntegration,
  setNotificationDefaults,
  testSlackWorkspaceIntegration,
  updateNotificationSubscription,
  updateNotificationTarget,
} from "../api";

vi.mock("../api", async () => {
  const actual = await vi.importActual<typeof import("../api")>("../api");
  return {
    ...actual,
    createNotificationSubscription: vi.fn(),
    createNotificationTarget: vi.fn(),
    deleteSlackWorkspaceIntegration: vi.fn(),
    deleteNotificationSubscription: vi.fn(),
    getNotificationDefaults: vi.fn(),
    getSlackWorkspaceIntegration: vi.fn(),
    listJobs: vi.fn(),
    listNotificationSubscriptions: vi.fn(),
    listNotificationTargets: vi.fn(),
    listProjects: vi.fn(),
    patchSlackWorkspaceIntegration: vi.fn(),
    putSlackWorkspaceIntegration: vi.fn(),
    setNotificationDefaults: vi.fn(),
    testSlackWorkspaceIntegration: vi.fn(),
    updateNotificationSubscription: vi.fn(),
    updateNotificationTarget: vi.fn(),
  };
});

function renderPage(options?: { isGlobalAdmin?: boolean }) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });

  const authValue: AuthContextValue = {
    currentUser: {
      id: "user-1",
      email: "admin@example.com",
      global_role: options?.isGlobalAdmin === false ? "user" : "admin",
    },
    authMode: "oidc",
    authStatus: "authenticated",
    error: null,
    isGlobalAdmin: options?.isGlobalAdmin !== false,
    loginAvailable: true,
    login: vi.fn(),
    logout: vi.fn(async () => {}),
    refreshCurrentUser: vi.fn(async () => {}),
  };

  return render(
    <AuthContext.Provider value={authValue}>
      <QueryClientProvider client={queryClient}>
        <NotificationsPage />
      </QueryClientProvider>
    </AuthContext.Provider>,
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
  const mockedDeleteSlackWorkspaceIntegration = vi.mocked(
    deleteSlackWorkspaceIntegration,
  );
  const mockedDeleteNotificationSubscription = vi.mocked(
    deleteNotificationSubscription,
  );
  const mockedGetNotificationDefaults = vi.mocked(getNotificationDefaults);
  const mockedGetSlackWorkspaceIntegration = vi.mocked(
    getSlackWorkspaceIntegration,
  );
  const mockedListJobs = vi.mocked(listJobs);
  const mockedListNotificationSubscriptions = vi.mocked(
    listNotificationSubscriptions,
  );
  const mockedListNotificationTargets = vi.mocked(listNotificationTargets);
  const mockedListProjects = vi.mocked(listProjects);
  const mockedPatchSlackWorkspaceIntegration = vi.mocked(
    patchSlackWorkspaceIntegration,
  );
  const mockedPutSlackWorkspaceIntegration = vi.mocked(
    putSlackWorkspaceIntegration,
  );
  const mockedSetNotificationDefaults = vi.mocked(setNotificationDefaults);
  const mockedTestSlackWorkspaceIntegration = vi.mocked(
    testSlackWorkspaceIntegration,
  );
  const mockedUpdateNotificationSubscription = vi.mocked(
    updateNotificationSubscription,
  );
  const mockedUpdateNotificationTarget = vi.mocked(updateNotificationTarget);

  beforeEach(() => {
    vi.clearAllMocks();

    mockedGetNotificationDefaults.mockResolvedValue({
      default_commit_author_failure_email_enabled: true,
      default_commit_author_success_email_enabled: false,
    });
    mockedGetSlackWorkspaceIntegration.mockResolvedValue({ configured: false });
    mockedPutSlackWorkspaceIntegration.mockResolvedValue({
      configured: true,
      integration: {
        id: "integration-1",
        workspace_id: "T123",
        workspace_name: "Coyote",
        linked_identity_count: 0,
        enabled: true,
        connected_at: "2026-07-01T00:00:00Z",
        updated_at: "2026-07-01T00:00:00Z",
      },
    });
    mockedPatchSlackWorkspaceIntegration.mockResolvedValue({
      configured: true,
      integration: {
        id: "integration-1",
        workspace_id: "T123",
        workspace_name: "Coyote",
        linked_identity_count: 0,
        enabled: true,
        connected_at: "2026-07-01T00:00:00Z",
        updated_at: "2026-07-01T00:00:00Z",
      },
    });
    mockedTestSlackWorkspaceIntegration.mockResolvedValue({
      configured: true,
      integration: {
        id: "integration-1",
        workspace_id: "T123",
        workspace_name: "Coyote",
        linked_identity_count: 0,
        enabled: true,
        connected_at: "2026-07-01T00:00:00Z",
        updated_at: "2026-07-01T00:00:00Z",
      },
    });
    mockedDeleteSlackWorkspaceIntegration.mockResolvedValue();

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
    mockedSetNotificationDefaults.mockResolvedValue({
      default_commit_author_failure_email_enabled: false,
      default_commit_author_success_email_enabled: true,
    });
  });

  it("renders notification defaults with clear non-retroactive copy", async () => {
    renderPage();

    await waitFor(() => {
      expect(
        screen.getByRole("checkbox", {
          name: /Notify new users when their commits fail/i,
        }),
      ).toBeChecked();
    });

    expect(screen.getByText(/does not modify existing users/i)).toBeTruthy();
    expect(
      screen.getByText(
        /currently applies only to personal email notifications/i,
      ),
    ).toBeTruthy();
    expect(screen.queryByText(/apply to existing users/i)).toBeNull();
    expect(
      screen.getByRole("checkbox", {
        name: /Notify new users when their commits succeed/i,
      }),
    ).not.toBeChecked();
  });

  it("updates notification defaults and prevents duplicate submits while pending", async () => {
    const pendingMutation = (() => {
      let release!: () => void;
      const promise = new Promise<{
        default_commit_author_failure_email_enabled: boolean;
        default_commit_author_success_email_enabled: boolean;
      }>((resolve) => {
        release = () => {
          resolve({
            default_commit_author_failure_email_enabled: false,
            default_commit_author_success_email_enabled: false,
          });
        };
      });
      return { promise, release };
    })();
    mockedSetNotificationDefaults.mockImplementation(
      () => pendingMutation.promise,
    );

    renderPage();

    const checkbox = await screen.findByRole("checkbox", {
      name: /Notify new users when their commits fail/i,
    });
    fireEvent.click(checkbox);

    await waitFor(() => {
      expect(mockedSetNotificationDefaults).toHaveBeenCalledTimes(1);
      expect(checkbox).toBeDisabled();
    });

    fireEvent.click(checkbox);
    expect(mockedSetNotificationDefaults).toHaveBeenCalledTimes(1);

    pendingMutation.release();
    await waitFor(() => {
      expect(mockedSetNotificationDefaults).toHaveBeenCalledWith(
        {
          default_commit_author_failure_email_enabled: false,
          default_commit_author_success_email_enabled: false,
        },
        expect.any(Object),
      );
    });
  });

  it("updates the success default independently", async () => {
    mockedSetNotificationDefaults.mockResolvedValue({
      default_commit_author_failure_email_enabled: true,
      default_commit_author_success_email_enabled: true,
    });

    renderPage();

    const successCheckbox = await screen.findByRole("checkbox", {
      name: /Notify new users when their commits succeed/i,
    });
    expect(successCheckbox).not.toBeChecked();

    fireEvent.click(successCheckbox);

    await waitFor(() => {
      expect(mockedSetNotificationDefaults).toHaveBeenCalledWith(
        {
          default_commit_author_failure_email_enabled: true,
          default_commit_author_success_email_enabled: true,
        },
        expect.any(Object),
      );
    });
  });

  it("preserves the prior visible default when the mutation fails", async () => {
    mockedSetNotificationDefaults.mockRejectedValue(new APIError(500, "boom"));

    renderPage();

    const checkbox = await screen.findByRole("checkbox", {
      name: /Notify new users when their commits fail/i,
    });
    expect(checkbox).toBeChecked();

    fireEvent.click(checkbox);

    await waitFor(() => {
      expect(
        screen.getByText(/Failed to update notification defaults/i),
      ).toBeTruthy();
    });
    expect(checkbox).toBeChecked();
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

  it("shows validation error when scope is missing", async () => {
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
    fireEvent.click(
      within(getCreateRuleForm()).getByRole("button", { name: "Create Rule" }),
    );

    await waitFor(() => {
      expect(screen.getByText("Scope is required.")).toBeTruthy();
    });
  });

  it("shows validation error when project is missing", async () => {
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
    fireEvent.click(
      within(getCreateRuleForm()).getByRole("button", { name: "Create Rule" }),
    );

    await waitFor(() => {
      expect(screen.getByText("Project is required.")).toBeTruthy();
    });
  });

  it("shows validation error when job is missing for job scope", async () => {
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
      target: { value: "job" },
    });
    fireEvent.change(within(getCreateRuleForm()).getByLabelText("4. Project"), {
      target: { value: "project-1" },
    });
    fireEvent.click(
      within(getCreateRuleForm()).getByRole("button", { name: "Create Rule" }),
    );

    await waitFor(() => {
      expect(screen.getByText("Job is required for job scope.")).toBeTruthy();
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

  it("editing a job-scoped rule updates enabled state in place", async () => {
    mockedListNotificationSubscriptions.mockResolvedValue([
      {
        id: "subscription-job-edit",
        target_id: "target-2",
        project_id: null,
        job_id: "job-2",
        event_type: "build_failed",
        enabled: false,
        created_at: "2026-06-01T00:00:00Z",
        updated_at: "2026-06-01T00:00:00Z",
      },
    ]);

    renderPage();

    await waitFor(() => {
      expect(
        within(getRulesSection()).getByText("Website / frontend-ci"),
      ).toBeTruthy();
    });

    clickEditForRule("Slack · #coyote-ci");
    fireEvent.click(within(getEditRulePanel()).getByLabelText("Enabled"));
    fireEvent.click(screen.getByRole("button", { name: "Save Rule" }));

    await waitFor(() => {
      expect(mockedUpdateNotificationSubscription).toHaveBeenCalledWith(
        "subscription-job-edit",
        { enabled: true },
      );
    });
  });

  it("editing a malformed job-scoped rule with no job id keeps project unset", async () => {
    mockedListNotificationSubscriptions.mockResolvedValue([
      {
        id: "subscription-missing-job-id",
        target_id: "target-2",
        project_id: null,
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
        within(getRulesSection()).getByText("One specific job"),
      ).toBeTruthy();
    });

    clickEditForRule("Slack · #coyote-ci");

    const projectSelect = within(getEditRulePanel()).getByLabelText(
      "Project",
    ) as HTMLSelectElement;
    expect(projectSelect.value).toBe("");
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

  it("manages Slack workspace integration and clears token after connect", async () => {
    renderPage();

    const tokenInput = (await screen.findByLabelText(
      "Slack bot token",
    )) as HTMLInputElement;
    const helpLink = screen.getByRole("link", { name: "Open Slack app setup" });
    expect(helpLink).toHaveAttribute("href", "https://api.slack.com/apps");
    expect(helpLink).toHaveAttribute("target", "_blank");
    expect(
      screen.queryByLabelText(
        "Allow this token to switch Coyote to a different Slack workspace.",
      ),
    ).toBeNull();
    fireEvent.change(tokenInput, { target: { value: "xoxb-secret" } });
    fireEvent.click(
      screen.getByRole("button", { name: "Connect Slack workspace" }),
    );

    await waitFor(() => {
      expect(mockedPutSlackWorkspaceIntegration).toHaveBeenCalledWith(
        expect.objectContaining({
          bot_token: "xoxb-secret",
          replace_existing: false,
        }),
        expect.anything(),
      );
    });

    await waitFor(() => {
      expect(tokenInput.value).toBe("");
      expect(screen.queryByDisplayValue("xoxb-secret")).toBeNull();
    });
  });

  it("renders a connected Slack workspace with null App ID and no unavailable metadata", async () => {
    mockedGetSlackWorkspaceIntegration.mockResolvedValue({
      configured: true,
      integration: {
        id: "integration-1",
        workspace_id: "T123",
        workspace_name: "Coyote",
        workspace_url: "https://coyote.slack.com/services",
        linked_identity_count: 0,
        enabled: true,
        connected_at: "2026-07-01T00:00:00Z",
        last_tested_at: "2026-07-01T14:46:00Z",
        last_test_succeeded: true,
        updated_at: "2026-07-01T00:00:00Z",
        app_id: null,
      },
    });

    renderPage();

    await screen.findByText("Coyote");
    expect(screen.getByText("Connected")).toBeTruthy();
    const workspaceLink = screen.getByRole("link", {
      name: "coyote.slack.com",
    });
    expect(workspaceLink).toHaveAttribute(
      "href",
      "https://coyote.slack.com/services",
    );
    expect(screen.getByText("Passed")).toBeTruthy();
    expect(screen.queryByText("App ID")).toBeNull();
    expect(screen.queryByText("Unavailable")).toBeNull();
    expect(screen.queryByLabelText("New Slack bot token")).toBeNull();
  });

  it("shows App ID only inside integration details when present", async () => {
    mockedGetSlackWorkspaceIntegration.mockResolvedValue({
      configured: true,
      integration: {
        id: "integration-1",
        workspace_id: "T123",
        workspace_name: "Coyote",
        workspace_url: "https://coyote.slack.com",
        bot_id: "B123",
        app_id: "A123",
        linked_identity_count: 0,
        enabled: true,
        connected_at: "2026-07-01T00:00:00Z",
        updated_at: "2026-07-01T00:00:00Z",
      },
    });

    renderPage();

    const detailsSummary = await screen.findByText("Integration details");
    const details = detailsSummary.closest("details");
    if (!details) {
      throw new Error("Expected integration details disclosure");
    }
    expect(within(details).getByText("Bot ID")).toBeTruthy();
    expect(within(details).getByText("B123")).toBeTruthy();
    expect(within(details).queryByText("Bot user ID")).toBeNull();
    expect(within(details).getByText("App ID")).toBeTruthy();
    expect(within(details).getByText("A123")).toBeTruthy();
  });

  it("shows Slack workspace loading and error states for admins", async () => {
    mockedGetSlackWorkspaceIntegration.mockImplementation(
      () => new Promise(() => {}),
    );

    renderPage();
    expect(
      await screen.findByText("Loading Slack workspace integration..."),
    ).toBeTruthy();
  });

  it("shows Slack workspace load errors", async () => {
    mockedGetSlackWorkspaceIntegration.mockRejectedValueOnce(
      new Error("load failed"),
    );

    renderPage();

    await waitFor(() => {
      expect(
        screen.getByText(
          /Failed to load Slack workspace integration: load failed/i,
        ),
      ).toBeTruthy();
    });
  });

  it("validates a missing Slack bot token before connect", async () => {
    renderPage();

    fireEvent.click(
      await screen.findByRole("button", { name: "Connect Slack workspace" }),
    );

    await waitFor(() => {
      expect(screen.getByText("Slack bot token is required.")).toBeTruthy();
    });
    expect(mockedPutSlackWorkspaceIntegration).not.toHaveBeenCalled();
  });

  it("shows disabled Slack workspace state and re-enables it", async () => {
    mockedGetSlackWorkspaceIntegration.mockResolvedValue({
      configured: true,
      integration: {
        id: "integration-1",
        workspace_id: "T123",
        workspace_name: "Coyote",
        linked_identity_count: 0,
        enabled: false,
        connected_at: "2026-07-01T00:00:00Z",
        updated_at: "2026-07-01T00:00:00Z",
      },
    });
    mockedPatchSlackWorkspaceIntegration.mockResolvedValueOnce({
      configured: true,
      integration: {
        id: "integration-1",
        workspace_id: "T123",
        workspace_name: "Coyote",
        linked_identity_count: 0,
        enabled: true,
        connected_at: "2026-07-01T00:00:00Z",
        updated_at: "2026-07-01T00:01:00Z",
      },
    });

    renderPage();

    await screen.findByRole("button", { name: "Enable integration" });
    expect(
      screen.getByText(/This workspace connection is paused/i),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Enable integration" }));

    await waitFor(() => {
      expect(mockedPatchSlackWorkspaceIntegration).toHaveBeenCalledWith(
        { enabled: true },
        expect.anything(),
      );
    });
  });

  it("shows failed Slack connection status and allows retesting", async () => {
    mockedGetSlackWorkspaceIntegration.mockResolvedValue({
      configured: true,
      integration: {
        id: "integration-1",
        workspace_id: "T123",
        workspace_name: "Coyote",
        linked_identity_count: 0,
        enabled: true,
        connected_at: "2026-07-01T00:00:00Z",
        last_tested_at: "2026-07-01T14:46:00Z",
        last_test_succeeded: false,
        updated_at: "2026-07-01T00:00:00Z",
      },
    });

    renderPage();

    await screen.findByText("Failed");
    fireEvent.click(screen.getByRole("button", { name: "Test connection" }));

    await waitFor(() => {
      expect(mockedTestSlackWorkspaceIntegration).toHaveBeenCalledWith(
        undefined,
        expect.anything(),
      );
    });
  });

  it("omits the workspace link when the Slack workspace URL is missing", async () => {
    mockedGetSlackWorkspaceIntegration.mockResolvedValue({
      configured: true,
      integration: {
        id: "integration-1",
        workspace_id: "T123",
        workspace_name: "Coyote",
        workspace_url: null,
        linked_identity_count: 0,
        enabled: true,
        connected_at: "2026-07-01T00:00:00Z",
        updated_at: "2026-07-01T00:00:00Z",
      },
    });

    renderPage();

    await screen.findByText("Coyote");
    expect(screen.queryByRole("link", { name: "coyote.slack.com" })).toBeNull();
  });

  it("reveals and cancels token replacement from the connected Slack workspace view", async () => {
    mockedGetSlackWorkspaceIntegration.mockResolvedValue({
      configured: true,
      integration: {
        id: "integration-1",
        workspace_id: "T123",
        workspace_name: "Coyote",
        workspace_url: "https://coyote.slack.com",
        linked_identity_count: 0,
        enabled: true,
        connected_at: "2026-07-01T00:00:00Z",
        updated_at: "2026-07-01T00:00:00Z",
      },
    });

    renderPage();

    await screen.findByText("Coyote");
    expect(screen.queryByLabelText("New Slack bot token")).toBeNull();
    expect(
      screen.queryByLabelText(
        "Allow this token to switch Coyote to a different Slack workspace.",
      ),
    ).toBeNull();

    const replaceButton = screen.getByRole("button", {
      name: "Replace bot token",
    });
    expect(replaceButton).toHaveClass("secondary-button");

    fireEvent.click(replaceButton);
    const tokenInput = (await screen.findByLabelText(
      "New Slack bot token",
    )) as HTMLInputElement;
    const switchCheckbox = screen.getByLabelText(
      "Allow this token to switch Coyote to a different Slack workspace.",
    ) as HTMLInputElement;
    const cancelButton = screen.getByRole("button", { name: "Cancel" });
    expect(cancelButton).toHaveClass("secondary-button");

    fireEvent.change(tokenInput, { target: { value: "xoxb-rotate" } });
    fireEvent.click(switchCheckbox);
    expect(switchCheckbox.checked).toBe(true);

    fireEvent.click(cancelButton);
    expect(screen.queryByLabelText("New Slack bot token")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Replace bot token" }));
    const tokenInputAfterCancel = (await screen.findByLabelText(
      "New Slack bot token",
    )) as HTMLInputElement;
    const switchCheckboxAfterCancel = screen.getByLabelText(
      "Allow this token to switch Coyote to a different Slack workspace.",
    ) as HTMLInputElement;
    expect(tokenInputAfterCancel.value).toBe("");
    expect(switchCheckboxAfterCancel.checked).toBe(false);
  });

  it("updates a connected Slack workspace token without requiring switch confirmation", async () => {
    mockedGetSlackWorkspaceIntegration.mockResolvedValue({
      configured: true,
      integration: {
        id: "integration-1",
        workspace_id: "T123",
        workspace_name: "Coyote",
        workspace_url: "https://coyote.slack.com",
        linked_identity_count: 0,
        enabled: true,
        connected_at: "2026-07-01T00:00:00Z",
        updated_at: "2026-07-01T00:00:00Z",
      },
    });

    renderPage();

    await screen.findByText("Coyote");
    fireEvent.click(screen.getByRole("button", { name: "Replace bot token" }));
    const tokenInput = (await screen.findByLabelText(
      "New Slack bot token",
    )) as HTMLInputElement;

    fireEvent.change(tokenInput, { target: { value: "xoxb-rotate" } });
    fireEvent.click(screen.getByRole("button", { name: "Save new token" }));

    await waitFor(() => {
      expect(mockedPutSlackWorkspaceIntegration).toHaveBeenCalledWith(
        expect.objectContaining({
          bot_token: "xoxb-rotate",
          replace_existing: false,
        }),
        expect.anything(),
      );
    });
  });

  it("shows action button hierarchy for a connected Slack workspace", async () => {
    mockedGetSlackWorkspaceIntegration.mockResolvedValue({
      configured: true,
      integration: {
        id: "integration-1",
        workspace_id: "T123",
        workspace_name: "Coyote",
        workspace_url: "https://coyote.slack.com",
        linked_identity_count: 0,
        enabled: true,
        connected_at: "2026-07-01T00:00:00Z",
        updated_at: "2026-07-01T00:00:00Z",
      },
    });

    renderPage();

    await screen.findByText("Coyote");
    expect(screen.getByRole("button", { name: "Test connection" })).toHaveClass(
      "secondary-button",
    );
    expect(
      screen.getByRole("button", { name: "Disable integration" }),
    ).toHaveClass("secondary-button", "danger-button");
    expect(screen.getByRole("button", { name: "Disconnect" })).toHaveClass(
      "secondary-button",
      "danger-button",
    );
  });

  it("sends replace_existing true only for an explicit confirmed workspace switch", async () => {
    mockedGetSlackWorkspaceIntegration.mockResolvedValue({
      configured: true,
      integration: {
        id: "integration-1",
        workspace_id: "T123",
        workspace_name: "Coyote",
        workspace_url: "https://coyote.slack.com",
        linked_identity_count: 0,
        enabled: true,
        connected_at: "2026-07-01T00:00:00Z",
        updated_at: "2026-07-01T00:00:00Z",
      },
    });

    renderPage();

    await screen.findByText("Coyote");
    fireEvent.click(screen.getByRole("button", { name: "Replace bot token" }));
    const tokenInput = (await screen.findByLabelText(
      "New Slack bot token",
    )) as HTMLInputElement;
    fireEvent.change(tokenInput, { target: { value: "xoxb-switch" } });
    fireEvent.click(
      screen.getByLabelText(
        "Allow this token to switch Coyote to a different Slack workspace.",
      ),
    );
    fireEvent.click(screen.getByRole("button", { name: "Save new token" }));

    await waitFor(() => {
      expect(mockedPutSlackWorkspaceIntegration).toHaveBeenCalledWith(
        expect.objectContaining({
          bot_token: "xoxb-switch",
          replace_existing: true,
        }),
        expect.anything(),
      );
    });
  });

  it("surfaces Slack workspace replacement conflicts and keeps current metadata visible", async () => {
    mockedGetSlackWorkspaceIntegration.mockResolvedValue({
      configured: true,
      integration: {
        id: "integration-1",
        workspace_id: "T123",
        workspace_name: "Coyote",
        workspace_url: "https://coyote.slack.com",
        linked_identity_count: 0,
        enabled: true,
        connected_at: "2026-07-01T00:00:00Z",
        updated_at: "2026-07-01T00:00:00Z",
      },
    });
    mockedPutSlackWorkspaceIntegration.mockRejectedValueOnce(
      new APIError(
        409,
        "slack workspace integration replacement requires explicit confirmation",
      ),
    );

    renderPage();

    await screen.findByText("Coyote");
    fireEvent.click(screen.getByRole("button", { name: "Replace bot token" }));
    const tokenInput = (await screen.findByLabelText(
      "New Slack bot token",
    )) as HTMLInputElement;
    fireEvent.change(tokenInput, { target: { value: "xoxb-other-workspace" } });
    fireEvent.click(screen.getByRole("button", { name: "Save new token" }));

    await waitFor(() => {
      expect(
        screen.getByText(
          /Failed to connect Slack workspace: API 409: slack workspace integration replacement requires explicit confirmation/i,
        ),
      ).toBeTruthy();
    });
    expect(screen.getByText("Coyote")).toBeTruthy();
    expect(screen.getByText("T123")).toBeTruthy();
  });

  it("does not disconnect the Slack workspace when the confirmation is canceled", async () => {
    mockedGetSlackWorkspaceIntegration.mockResolvedValue({
      configured: true,
      integration: {
        id: "integration-1",
        workspace_id: "T123",
        workspace_name: "Coyote",
        linked_identity_count: 2,
        enabled: true,
        connected_at: "2026-07-01T00:00:00Z",
        updated_at: "2026-07-01T00:00:00Z",
      },
    });
    const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(false);

    renderPage();

    await screen.findByText("Coyote");
    expect(screen.getByText("2 linked personal Slack identities")).toBeTruthy();
    expect(
      screen.getByText(
        /This workspace has linked user identities\. Unlink them before disconnecting or switching workspaces\./i,
      ),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Disconnect" }));

    expect(confirmSpy).toHaveBeenCalled();
    expect(mockedDeleteSlackWorkspaceIntegration).not.toHaveBeenCalled();
  });

  it("disconnects the Slack workspace when the confirmation is accepted", async () => {
    mockedGetSlackWorkspaceIntegration.mockResolvedValue({
      configured: true,
      integration: {
        id: "integration-1",
        workspace_id: "T123",
        workspace_name: "Coyote",
        linked_identity_count: 0,
        enabled: true,
        connected_at: "2026-07-01T00:00:00Z",
        updated_at: "2026-07-01T00:00:00Z",
      },
    });
    const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(true);

    renderPage();

    await screen.findByText("Coyote");
    fireEvent.click(screen.getByRole("button", { name: "Disconnect" }));

    expect(confirmSpy).toHaveBeenCalled();
    await waitFor(() => {
      expect(mockedDeleteSlackWorkspaceIntegration).toHaveBeenCalledWith(
        undefined,
        expect.anything(),
      );
    });
  });

  it("hides Slack workspace controls for non-admin users", async () => {
    renderPage({ isGlobalAdmin: false });

    await waitFor(() => {
      expect(
        screen.getByText(
          "Global admin access is required to manage Slack workspace integration.",
        ),
      ).toBeTruthy();
    });
    expect(screen.queryByLabelText("Slack bot token")).toBeNull();
    expect(
      screen.queryByRole("button", { name: "Replace bot token" }),
    ).toBeNull();
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
