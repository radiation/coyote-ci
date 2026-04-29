import { useEffect, useMemo, useState, type ReactNode } from "react";
import {
  applyTheme,
  resolveInitialTheme,
  THEME_MEDIA_QUERY,
  THEME_STORAGE_KEY,
  ThemeContext,
  type ThemeContextValue,
} from "./theme-context";

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [{ preference, systemTheme }, setThemeState] =
    useState(resolveInitialTheme);

  const theme = preference ?? systemTheme;

  useEffect(() => {
    applyTheme(theme);
  }, [theme]);

  useEffect(() => {
    if (typeof window === "undefined") {
      return;
    }

    if (preference) {
      return;
    }

    if (typeof window.matchMedia !== "function") {
      return;
    }

    const mediaQueryList = window.matchMedia(THEME_MEDIA_QUERY);
    const handleChange = (event: MediaQueryListEvent) => {
      setThemeState((current) => ({
        ...current,
        systemTheme: event.matches ? "dark" : "light",
      }));
    };

    mediaQueryList.addEventListener("change", handleChange);

    return () => {
      mediaQueryList.removeEventListener("change", handleChange);
    };
  }, [preference]);

  useEffect(() => {
    if (typeof window === "undefined") {
      return;
    }

    try {
      if (preference) {
        window.localStorage.setItem(THEME_STORAGE_KEY, preference);
      } else {
        window.localStorage.removeItem(THEME_STORAGE_KEY);
      }
    } catch {
      // Ignore storage write failures and continue with the in-memory theme.
    }
  }, [preference]);

  const value = useMemo<ThemeContextValue>(
    () => ({
      theme,
      setTheme: (nextTheme) => {
        setThemeState((current) => ({
          ...current,
          preference: nextTheme,
        }));
      },
      toggleTheme: () => {
        setThemeState((current) => ({
          ...current,
          preference:
            (current.preference ?? current.systemTheme) === "dark"
              ? "light"
              : "dark",
        }));
      },
    }),
    [theme],
  );

  return (
    <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>
  );
}
