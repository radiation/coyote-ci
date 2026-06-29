import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider, createMemoryRouter } from "react-router-dom";
import { ThemeProvider } from "../theme";
import { AuthProvider } from "../auth";
import { appRoutes } from "./router";
import {
  getCommitAuthorFailureNotificationPreference,
  getCommitAuthorSuccessNotificationPreference,
  getAuthConfig,
  getMe,
  getMyEmailNotificationTarget,
  listBuilds,
  listProjects,
  listQueue,
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

  return render(
    <QueryClientProvider client={queryClient}>
      <ThemeProvider>
        <AuthProvider>
          <RouterProvider router={router} />
        </AuthProvider>
      </ThemeProvider>
    </QueryClientProvider>,
  );
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
  });

  it("redirects the home route to the dashboard", async () => {
    renderRouter(["/"]);

    expect(
      await screen.findByRole("heading", { name: "Dashboard" }),
    ).toBeTruthy();
    expect(screen.getByRole("link", { name: "Dashboard" })).toHaveClass(
      "is-active",
    );
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
});
