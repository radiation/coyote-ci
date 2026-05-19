import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider, createMemoryRouter } from "react-router-dom";
import { ThemeProvider } from "../theme";
import { AuthProvider } from "../auth";
import { appRoutes } from "./router";
import {
  getAuthConfig,
  getMe,
  listBuilds,
  listProjects,
  listQueue,
} from "../api";
import { installMockLocalStorage } from "../test/browserMocks";

vi.mock("../api", async () => {
  const actual = await vi.importActual<typeof import("../api")>("../api");
  return {
    ...actual,
    getAuthConfig: vi.fn(),
    getMe: vi.fn(),
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
  const router = createMemoryRouter(
    appRoutes as Parameters<typeof createMemoryRouter>[0],
    {
      initialEntries,
    },
  );

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
  const mockedGetMe = vi.mocked(getMe);
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
      user: {
        id: "disabled-mode-user",
        email: "dev@local.coyote-ci",
        global_role: "admin",
      },
    });
    mockedListProjects.mockResolvedValue([]);
    mockedListQueue.mockResolvedValue([]);
    mockedListBuilds.mockResolvedValue([]);
  });

  it("redirects the home route to the dashboard", async () => {
    renderRouter(["/"]);

    await waitFor(() => {
      expect(screen.getByRole("link", { name: "Dashboard" })).toHaveClass(
        "is-active",
      );
      expect(screen.getByText("Where should I look right now?")).toBeTruthy();
    });
  });
});
