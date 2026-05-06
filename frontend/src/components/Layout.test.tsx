import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { Layout } from "./Layout";
import { getMe } from "../api";
import { ThemeProvider } from "../theme";
import { installMockLocalStorage } from "../test/browserMocks";
import { THEME_STORAGE_KEY } from "../theme-context";

vi.mock("../api", () => ({
  getMe: vi.fn(),
}));

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
    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
      },
    });

    render(
      <QueryClientProvider client={queryClient}>
        <ThemeProvider>
          <MemoryRouter initialEntries={["/artifacts"]}>
            <Routes>
              <Route element={<Layout />}>
                <Route path="/artifacts" element={<div>Artifacts page</div>} />
              </Route>
            </Routes>
          </MemoryRouter>
        </ThemeProvider>
      </QueryClientProvider>,
    );

    const toggle = screen.getByRole("button", {
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
    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
      },
    });

    render(
      <QueryClientProvider client={queryClient}>
        <ThemeProvider>
          <MemoryRouter initialEntries={["/artifacts"]}>
            <Routes>
              <Route element={<Layout />}>
                <Route path="/artifacts" element={<div>Artifacts page</div>} />
              </Route>
            </Routes>
          </MemoryRouter>
        </ThemeProvider>
      </QueryClientProvider>,
    );

    await waitFor(() => {
      expect(screen.queryByRole("link", { name: "Users" })).toBeNull();
    });
  });
});
