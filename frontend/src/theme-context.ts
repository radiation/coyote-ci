import { createContext, useContext } from "react";

export type Theme = "light" | "dark";

type ThemeContextValue = {
  theme: Theme;
  setTheme: (theme: Theme) => void;
  toggleTheme: () => void;
};

export const THEME_MEDIA_QUERY = "(prefers-color-scheme: dark)";

export const THEME_STORAGE_KEY = "coyote-theme";

export const ThemeContext = createContext<ThemeContextValue | null>(null);

export function readStoredThemePreference(
  storage: Storage | undefined,
): Theme | null {
  if (!storage) {
    return null;
  }

  try {
    const value = storage.getItem(THEME_STORAGE_KEY);
    return value === "light" || value === "dark" ? value : null;
  } catch {
    return null;
  }
}

export function getSystemTheme(
  mediaQueryList?: Pick<MediaQueryList, "matches"> | null,
): Theme {
  return mediaQueryList?.matches ? "dark" : "light";
}

export function resolveInitialTheme(): {
  preference: Theme | null;
  systemTheme: Theme;
} {
  if (typeof window === "undefined") {
    return { preference: null, systemTheme: "light" };
  }

  const systemTheme = getSystemTheme(
    typeof window.matchMedia === "function"
      ? window.matchMedia(THEME_MEDIA_QUERY)
      : null,
  );
  const preference = readStoredThemePreference(window.localStorage);

  return { preference, systemTheme };
}

export function applyTheme(
  theme: Theme,
  root = document.documentElement,
): void {
  root.dataset.theme = theme;
  root.style.colorScheme = theme;
}

export function useTheme(): ThemeContextValue {
  const context = useContext(ThemeContext);
  if (!context) {
    throw new Error("useTheme must be used within a ThemeProvider");
  }
  return context;
}
