import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { Layout } from "./Layout";
import { APIError, getMe } from "../api";
import { AuthProvider } from "../auth";
import { ThemeProvider } from "../theme";
import { installMockLocalStorage } from "../test/browserMocks";
import { THEME_STORAGE_KEY } from "../theme-context";

vi.mock("../api", async () => {
  const actual = await vi.importActual<typeof import("../api")>("../api");
  return {
    ...actual,
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

function renderLayout(navigate = vi.fn()) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
    },
  });

  render(
    <QueryClientProvider client={queryClient}>
      <ThemeProvider>
        <AuthProvider navigate={navigate}>
          <MemoryRouter initialEntries={["/artifacts"]}>
            <Routes>
              <Route element={<Layout />}>
                <Route path="/artifacts" element={<div>Artifacts page</div>} />
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
  const mockedGetMe = vi.mocked(getMe);

  beforeEach(() => {
    installMockLocalStorage();
    window.localStorage.clear();
    document.documentElement.removeAttribute("data-theme");
    document.documentElement.style.colorScheme = "";
    installMatchMedia(false);
    mockedGetMe.mockResolvedValue({
      auth_mode: "disabled",
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
    expect(screen.getByRole("link", { name: "Artifacts" })).toHaveClass(
      "is-active",
    );
    await waitFor(() => {
      expect(screen.getByRole("link", { name: "Users" })).toBeTruthy();
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
      user: {
        id: "user-1",
        email: "user@example.com",
        global_role: "user",
      },
    });
    renderLayout();

    await waitFor(() => {
      expect(screen.queryByRole("link", { name: "Users" })).toBeNull();
    });
  });

  it("shows sign-in UI when /me returns 401", async () => {
    mockedGetMe.mockRejectedValue(
      new APIError(401, "missing user email header"),
    );

    const { navigate } = renderLayout();

    await waitFor(() => {
      expect(screen.getByText("Sign in to Coyote CI")).toBeTruthy();
    });

    fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
    expect(navigate).toHaveBeenCalledWith("/auth/login");
  });
});
