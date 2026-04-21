import { useEffect, useState } from "react";

export type ThemeMode = "light" | "dark";
const STORAGE_KEY = "sanderling-inspect-theme";

function detectInitial(): ThemeMode {
  if (typeof window === "undefined") {
    return "light";
  }
  const stored = window.localStorage.getItem(STORAGE_KEY);
  if (stored === "light" || stored === "dark") {
    return stored;
  }
  if (window.matchMedia && window.matchMedia("(prefers-color-scheme: dark)").matches) {
    return "dark";
  }
  return "light";
}

export function useTheme(): { theme: ThemeMode; toggle: () => void } {
  const [theme, setTheme] = useState<ThemeMode>(() => detectInitial());

  useEffect(() => {
    if (typeof document === "undefined") {
      return;
    }
    document.documentElement.dataset.theme = theme;
    try {
      window.localStorage.setItem(STORAGE_KEY, theme);
    } catch {
      // private mode or storage disabled — theme still applies for the session.
    }
  }, [theme]);

  return {
    theme,
    toggle: () => setTheme((current) => (current === "dark" ? "light" : "dark")),
  };
}
