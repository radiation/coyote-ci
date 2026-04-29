import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { ThemeProvider } from "./theme";
import { installMockLocalStorage } from "./test/browserMocks";
import { THEME_STORAGE_KEY, useTheme } from "./theme-context";

type MatchMediaListener = (event: MediaQueryListEvent) => void;

function installMatchMedia(initialMatches: boolean) {
  let matches = initialMatches;
  const listeners = new Set<MatchMediaListener>();

  const matchMedia = vi.fn().mockImplementation((query: string) => ({
    matches,
    media: query,
    onchange: null,
    addEventListener: (_eventName: string, listener: MatchMediaListener) => {
      listeners.add(listener);
    },
    removeEventListener: (_eventName: string, listener: MatchMediaListener) => {
      listeners.delete(listener);
    },
    addListener: (listener: MatchMediaListener) => {
      listeners.add(listener);
    },
    removeListener: (listener: MatchMediaListener) => {
      listeners.delete(listener);
    },
    dispatchEvent: () => true,
  }));

  Object.defineProperty(window, "matchMedia", {
    configurable: true,
    writable: true,
    value: matchMedia,
  });

  return {
    setMatches(nextMatches: boolean) {
      matches = nextMatches;
      const event = { matches: nextMatches } as MediaQueryListEvent;
      for (const listener of listeners) {
        listener(event);
      }
    },
  };
}

function ThemeProbe() {
  const { theme, toggleTheme } = useTheme();

  return (
    <>
      <output data-testid="theme-value">{theme}</output>
      <button type="button" onClick={toggleTheme}>
        Toggle theme
      </button>
    </>
  );
}

describe("ThemeProvider", () => {
  beforeEach(() => {
    installMockLocalStorage();
    window.localStorage.clear();
    document.documentElement.removeAttribute("data-theme");
    document.documentElement.style.colorScheme = "";
  });

  it("uses the saved theme preference and persists toggle changes", async () => {
    window.localStorage.setItem(THEME_STORAGE_KEY, "dark");
    installMatchMedia(false);

    render(
      <ThemeProvider>
        <ThemeProbe />
      </ThemeProvider>,
    );

    await waitFor(() => {
      expect(screen.getByTestId("theme-value")).toHaveTextContent("dark");
      expect(document.documentElement).toHaveAttribute("data-theme", "dark");
    });

    fireEvent.click(screen.getByRole("button", { name: "Toggle theme" }));

    await waitFor(() => {
      expect(screen.getByTestId("theme-value")).toHaveTextContent("light");
      expect(document.documentElement).toHaveAttribute("data-theme", "light");
      expect(window.localStorage.getItem(THEME_STORAGE_KEY)).toBe("light");
    });
  });

  it("falls back to the system preference when no saved theme exists", async () => {
    const media = installMatchMedia(true);

    render(
      <ThemeProvider>
        <ThemeProbe />
      </ThemeProvider>,
    );

    await waitFor(() => {
      expect(screen.getByTestId("theme-value")).toHaveTextContent("dark");
      expect(document.documentElement).toHaveAttribute("data-theme", "dark");
      expect(window.localStorage.getItem(THEME_STORAGE_KEY)).toBeNull();
    });

    act(() => {
      media.setMatches(false);
    });

    await waitFor(() => {
      expect(screen.getByTestId("theme-value")).toHaveTextContent("light");
      expect(document.documentElement).toHaveAttribute("data-theme", "light");
    });
  });
});
