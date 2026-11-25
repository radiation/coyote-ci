import { beforeEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider, createMemoryRouter } from "react-router-dom";
import { ThemeProvider } from "../theme";
import { AuthProvider } from "../auth";
import { appRoutes } from "./router";
import {
  APIError,
  getCommitAuthorFailureNotificationPreference,
  getCommitAuthorSuccessNotificationPreference,
  getAuthConfig,
  getMe,
  getMyEmailNotificationTarget,
  listBuilds,
  listProjects,
  listQueue,
  listWorkers,
} from "../api";
import { installMockLocalStorage } from "../test/browserMocks";

vi.mock("../api", async () => {
  const actual = await vi.importActual<typeof import("../api")>("../api");
  return {
    ...actual,
    getCommitAuthorFailureNotificationPreference: vi.fn(),
    getCommitAuthorSuccessNotificationPreference: vi.fn(),
    getAuthConfig: vi.fn(),
    getMe: vi.fn(),
    getMyEmailNotificationTarget: vi.fn(),
    listProjects: vi.fn(),
    listQueue: vi.fn(),
    listBuilds: vi.fn(),
    listWorkers: vi.fn(),
  };
});

function installMatchMedia(initialMatches: boolean) {
  Object.defineProperty(window, "matchMedia", {
    configurable: true,
    writable: true,
    value: vi.fn().mockImplementation((query: string) => ({
      matches: initialMatches,
      media: query,
      onchange: null,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      addListener: vi.fn(),
      removeListener: vi.fn(),
      dispatchEvent: vi.fn(),
    })),
  });
}

function renderRouter(initialEntries: string[]) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
    },
  });
  const router = createMemoryRouter(appRoutes, {
    initialEntries,
  });

  render(
    <QueryClientProvider client={queryClient}>
      <ThemeProvider>
        <AuthProvider>
          <RouterProvider router={router} />
        </AuthProvider>
      </ThemeProvider>
    </QueryClientProvider>,
  );
  return router;
}

describe("router dashboard home", () => {
  const mockedGetAuthConfig = vi.mocked(getAuthConfig);
  const mockedGetCommitAuthorFailureNotificationPreference = vi.mocked(
    getCommitAuthorFailureNotificationPreference,
  );
  const mockedGetCommitAuthorSuccessNotificationPreference = vi.mocked(
    getCommitAuthorSuccessNotificationPreference,
  );
  const mockedGetMe = vi.mocked(getMe);
  const mockedGetMyEmailNotificationTarget = vi.mocked(
    getMyEmailNotificationTarget,
  );
  const mockedListProjects = vi.mocked(listProjects);
  const mockedListQueue = vi.mocked(listQueue);
  const mockedListBuilds = vi.mocked(listBuilds);

  beforeEach(() => {
    installMockLocalStorage();
    window.localStorage.clear();
    document.documentElement.removeAttribute("data-theme");
    document.documentElement.style.colorScheme = "";
    installMatchMedia(false);
    mockedGetAuthConfig.mockResolvedValue({
      auth_mode: "disabled",
      login_url: null,
    });
    mockedGetMe.mockResolvedValue({
      auth_mode: "disabled",
      email_verified: null,
      user: {
        id: "disabled-mode-user",
        email: "dev@local.coyote-ci",
        global_role: "admin",
      },
    });
    mockedGetMyEmailNotificationTarget.mockResolvedValue(null);
    mockedGetCommitAuthorFailureNotificationPreference.mockResolvedValue({
      enabled: false,
      eligible: false,
      delivery_active: false,
      target: null,
      unavailable_reason: "personal_target_required",
    });
    mockedGetCommitAuthorSuccessNotificationPreference.mockResolvedValue({
      enabled: false,
      eligible: false,
      delivery_active: false,
      target: null,
      unavailable_reason: "personal_target_required",
    });
    mockedListProjects.mockResolvedValue([]);
    mockedListQueue.mockResolvedValue([]);
    mockedListBuilds.mockResolvedValue([]);
    vi.mocked(listWorkers).mockResolvedValue([]);
  });

  it("redirects the home route to the dashboard", async () => {
    renderRouter(["/"]);

    expect(
      await screen.findByRole("heading", { name: "Overview" }),
    ).toBeTruthy();
    expect(screen.getByRole("link", { name: "Overview" })).toHaveClass(
      "is-active",
    );
  });

  it("redirects anonymous Overview visits without requesting dashboard data", async () => {
    vi.clearAllMocks();
    mockedGetAuthConfig.mockResolvedValue({
      auth_mode: "oidc",
      login_url: "/auth/login",
    });
    mockedGetMe.mockRejectedValue(new APIError(401, "unauthenticated"));
    const router = renderRouter(["/dashboard"]);
    await screen.findByRole("link", { name: "Projects" });
    expect(router.state.location.pathname).toBe("/projects");
    expect(mockedListQueue).not.toHaveBeenCalled();
    expect(mockedListBuilds).not.toHaveBeenCalled();
    expect(listWorkers).not.toHaveBeenCalled();
  });

  it("restores the filtered Activity view and fragment on Back", async () => {
    const router = renderRouter(["/builds?project_id=p1#recent"]);
    fireEvent.click(await screen.findByRole("link", { name: "Overview" }));
    expect(
      await screen.findByRole("heading", { name: "Overview" }),
    ).toBeTruthy();
    await act(() => router.navigate(-1));
    expect(
      await screen.findByRole("navigation", { name: "Activity views" }),
    ).toBeTruthy();
    expect(router.state.location).toMatchObject({
      pathname: "/builds",
      search: "?project_id=p1",
      hash: "#recent",
    });
    expect(
      screen.getByRole("link", { name: "Builds", current: "page" }),
    ).toBeTruthy();
  });

  it("renders the projects route inside the shared shell", async () => {
    renderRouter(["/projects"]);

    expect(
      await screen.findByRole("heading", { name: "Projects" }),
    ).toBeTruthy();
    expect(screen.getByRole("link", { name: "Projects" })).toHaveClass(
      "is-active",
    );
    expect(
      screen.getByRole("complementary", {
        name: "Application navigation",
      }),
    ).toBeTruthy();
  });

  it("renders the profile settings route", async () => {
    renderRouter(["/settings/profile"]);

    expect(
      await screen.findByRole("heading", { name: "Profile" }),
    ).toBeTruthy();
    expect(screen.getByRole("link", { name: "Profile" })).toHaveClass(
      "is-active",
    );
  });

  it("renders the my notifications settings route", async () => {
    renderRouter(["/settings/my-notifications"]);

    expect(
      await screen.findByRole("heading", { name: "My notifications" }),
    ).toBeTruthy();
    expect(screen.getByRole("link", { name: "My notifications" })).toHaveClass(
      "is-active",
    );
  });

  it.each([
    "/settings/tokens",
    "/settings/api-tokens",
    "/settings/my-api-tokens",
  ])("resolves %s in Settings", async (path) => {
    const router = renderRouter([path]);

    expect(
      await screen.findByRole("heading", { name: "My API Tokens" }),
    ).toBeTruthy();
    expect(screen.queryByRole("link", { name: "My API Tokens" })).toBeNull();
    expect(router.state.location.pathname).toBe("/settings/my-api-tokens");
    expect(
      screen.getByRole("link", { name: "Settings", current: "location" }),
    ).toBeTruthy();
  });
});
