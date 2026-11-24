import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { Layout } from "./Layout";
import { APIError, getAuthConfig, getMe } from "../api";
import { AuthProvider } from "../auth";
import { ThemeProvider } from "../theme";
import { installMockLocalStorage } from "../test/browserMocks";
import { THEME_STORAGE_KEY } from "../theme-context";

declare const __COYOTE_VERSION__: string;

vi.mock("../api", async () => {
  const actual = await vi.importActual<typeof import("../api")>("../api");
  return {
    ...actual,
    getAuthConfig: vi.fn(),
    getMe: vi.fn(),
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

function renderLayout(initialEntry = "/artifacts", navigate = vi.fn()) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
    },
  });

  render(
    <QueryClientProvider client={queryClient}>
      <ThemeProvider>
        <AuthProvider navigate={navigate}>
          <MemoryRouter initialEntries={[initialEntry]}>
            <Routes>
              <Route element={<Layout />}>
                <Route path="/artifacts" element={<div>Artifacts page</div>} />
                <Route path="/projects" element={<div>Projects page</div>} />
                <Route path="*" element={<div>Destination</div>} />
              </Route>
            </Routes>
          </MemoryRouter>
        </AuthProvider>
      </ThemeProvider>
    </QueryClientProvider>,
  );

  return { navigate };
}

describe("Layout", () => {
  const mockedGetAuthConfig = vi.mocked(getAuthConfig);
  const mockedGetMe = vi.mocked(getMe);

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
  });

  it("renders a persistent theme toggle and updates the preference", async () => {
    renderLayout();

    const toggle = await screen.findByRole("button", {
      name: "Switch to dark theme",
    });

    expect(toggle).toBeTruthy();
    await waitFor(() => {
      expect(screen.getByRole("link", { name: "Artifacts" })).toHaveClass(
        "is-active",
      );
      expect(screen.getByLabelText("Application version")).toHaveTextContent(
        `Coyote CI ${__COYOTE_VERSION__}`,
      );
      expect(
        screen.queryByRole("navigation", { name: "Settings views" }),
      ).toBeNull();
    });

    fireEvent.click(toggle);

    await waitFor(() => {
      expect(document.documentElement).toHaveAttribute("data-theme", "dark");
      expect(window.localStorage.getItem(THEME_STORAGE_KEY)).toBe("dark");
      expect(
        screen.getByRole("button", { name: "Switch to light theme" }),
      ).toBeTruthy();
    });
  });

  it("hides the Users nav link for non-admin header-mode identities", async () => {
    mockedGetMe.mockResolvedValue({
      auth_mode: "header",
      email_verified: null,
      user: {
        id: "user-1",
        email: "user@example.com",
        global_role: "user",
      },
    });
    renderLayout("/settings/profile");

    await waitFor(() => {
      expect(
        screen.getByRole("link", { name: "My notifications" }),
      ).toBeTruthy();
      expect(screen.queryByRole("link", { name: "Notifications" })).toBeNull();
      expect(screen.queryByRole("link", { name: "Users" })).toBeNull();
      expect(screen.getByRole("link", { name: "My API Tokens" })).toBeTruthy();
    });
  });

  it.each([
    ["/builds", "Activity", "Builds", "page"],
    ["/builds/run-1?step=0#step-0", "Activity", "Builds", "location"],
    ["/queue?project_id=1", "Activity", "Queue", "page"],
    ["/workers", "Activity", "Workers", "page"],
    ["/projects/p1", "Projects", "Project directory", "location"],
    ["/projects/public/builds/b1", "Projects", "Project directory", "location"],
    ["/jobs/new", "Projects", "All jobs", "location"],
    ["/jobs/j1", "Projects", "All jobs", "location"],
    ["/settings/users", "Settings", "Users", "page"],
  ])(
    "resolves navigation on direct entry to %s",
    async (url, section, view, marker) => {
      renderLayout(url);
      const primary = within(
        await screen.findByRole("navigation", { name: "Primary" }),
      );
      expect(
        primary
          .getAllByRole("link")
          .map((link) => [link.textContent, link.getAttribute("href")]),
      ).toEqual([
        ["Overview", "/dashboard"],
        ["Projects", "/projects"],
        ["Activity", "/builds"],
        ["Artifacts", "/artifacts"],
        ["Settings", "/settings/profile"],
      ]);
      expect(
        primary.getByRole("link", { name: section, current: "location" }),
      ).toBeTruthy();
      const secondary = within(
        screen.getByRole("navigation", { name: `${section} views` }),
      );
      const names =
        section === "Activity"
          ? ["Builds", "Queue", "Workers"]
          : section === "Projects"
            ? ["Project directory", "All jobs"]
            : [
                "Profile",
                "My notifications",
                "Credentials",
                "Notifications",
                "Users",
              ];
      expect(
        secondary.getAllByRole("link").map((link) => link.textContent),
      ).toEqual(names);
      const link = secondary.getByRole("link", { name: view });
      expect(link).toHaveAttribute("aria-current", marker);
      link.focus();
      expect(link).toHaveFocus();
      expect(link.tabIndex).toBe(0);
    },
  );

  it("allows anonymous visitors through the public shell and shows sign-in", async () => {
    mockedGetAuthConfig.mockResolvedValue({
      auth_mode: "oidc",
      login_url: "/auth/login",
    });
    mockedGetMe.mockRejectedValue(
      new APIError(401, "missing user email header"),
    );

    const { navigate } = renderLayout();

    await waitFor(() => {
      expect(screen.getByRole("link", { name: "Projects" })).toBeTruthy();
    });

    expect(screen.queryByRole("link", { name: "Artifacts" })).toBeNull();
    expect(screen.queryByRole("link", { name: "Queue" })).toBeNull();
    expect(screen.queryByRole("link", { name: "Workers" })).toBeNull();
    expect(screen.queryByRole("link", { name: "Users" })).toBeNull();

    expect(screen.getAllByRole("navigation")).toHaveLength(1);
    expect(screen.queryByRole("link", { name: "All jobs" })).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
    expect(navigate).toHaveBeenCalledWith("/auth/login");
  });

  it("shows loading state while auth config is still loading", () => {
    mockedGetAuthConfig.mockImplementation(
      () => new Promise(() => undefined) as Promise<never>,
    );

    renderLayout();

    expect(screen.getByText("Loading session")).toBeTruthy();
    expect(screen.getByText("Checking your Coyote CI session.")).toBeTruthy();
  });

  it("keeps the public shell available without sign-in for header-mode 401", async () => {
    mockedGetAuthConfig.mockResolvedValue({
      auth_mode: "header",
      login_url: null,
    });
    mockedGetMe.mockRejectedValue(
      new APIError(401, "missing user email header"),
    );

    renderLayout();

    await waitFor(() => {
      expect(screen.getByRole("link", { name: "Projects" })).toBeTruthy();
    });

    expect(screen.queryByRole("button", { name: "Sign in" })).toBeNull();
  });

  it("shows retry UI when the current session cannot be loaded", async () => {
    mockedGetMe.mockRejectedValue(new APIError(500, "session backend failed"));

    renderLayout();

    await waitFor(() => {
      expect(screen.getByText("Unable to load session")).toBeTruthy();
      expect(screen.getByText("API 500: session backend failed")).toBeTruthy();
    });

    const initialCalls = mockedGetMe.mock.calls.length;

    fireEvent.click(screen.getByRole("button", { name: "Retry" }));

    await waitFor(() => {
      expect(mockedGetMe.mock.calls.length).toBeGreaterThan(initialCalls);
    });
  });

  it("shows the logout control for authenticated oidc users", async () => {
    mockedGetMe.mockResolvedValue({
      auth_mode: "oidc",
      email_verified: null,
      user: {
        id: "user-1",
        email: "user@example.com",
        display_name: "User Example",
        global_role: "admin",
      },
    });

    renderLayout();

    await waitFor(() => {
      expect(screen.getByText("User Example")).toBeTruthy();
      expect(screen.getByRole("button", { name: "Logout" })).toBeTruthy();
    });
  });
});
