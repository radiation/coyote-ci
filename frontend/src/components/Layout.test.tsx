import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { Layout } from "./Layout";
import { ThemeProvider } from "../theme";
import { installMockLocalStorage } from "../test/browserMocks";
import { THEME_STORAGE_KEY } from "../theme-context";

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
  beforeEach(() => {
    installMockLocalStorage();
    window.localStorage.clear();
    document.documentElement.removeAttribute("data-theme");
    document.documentElement.style.colorScheme = "";
    installMatchMedia(false);
  });

  it("renders a persistent theme toggle and updates the preference", async () => {
    render(
      <ThemeProvider>
        <MemoryRouter initialEntries={["/artifacts"]}>
          <Routes>
            <Route element={<Layout />}>
              <Route path="/artifacts" element={<div>Artifacts page</div>} />
            </Route>
          </Routes>
        </MemoryRouter>
      </ThemeProvider>,
    );

    const toggle = screen.getByRole("button", {
      name: "Switch to dark theme",
    });

    expect(toggle).toBeTruthy();
    expect(screen.getByRole("link", { name: "Artifacts" })).toHaveClass(
      "is-active",
    );

    fireEvent.click(toggle);

    await waitFor(() => {
      expect(document.documentElement).toHaveAttribute("data-theme", "dark");
      expect(window.localStorage.getItem(THEME_STORAGE_KEY)).toBe("dark");
      expect(
        screen.getByRole("button", { name: "Switch to light theme" }),
      ).toBeTruthy();
    });
  });
});
