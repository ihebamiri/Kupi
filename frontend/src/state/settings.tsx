import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { Window } from "@wailsio/runtime";
import { SettingsService, errorMessage, type Settings, type SettingsView } from "../lib/api";

// Used until the real settings arrive (and in tests): the same defaults as Go.
export const DEFAULT_SETTINGS: Settings = {
  theme: "system",
  uiScale: 100,
  logTailLines: 500,
  logTimestamps: false,
  mapRefreshSeconds: 15,
  mapShowPods: true,
  clipboardClearSeconds: 30,
  vaultAutoLockMinutes: 15,
  historyRetentionDays: 14,
  terminalEnabled: false,
  terminalShell: "",
  terminalFontSize: 13,
  terminalScrollback: 10000,
  terminalPasteGuard: true,
  terminalFollowCluster: true,
  terminalCopyOnSelect: false,
  snippets: [],
};

interface SettingsState {
  settings: Settings;
  view: SettingsView | null;
  error: string | null;
  /** Saves a change; resolves to an error message, or null when saved. */
  save: (patch: Partial<Settings>) => Promise<string | null>;
  /** Asks in a native dialog, then switches the terminal on. */
  enableTerminal: () => Promise<boolean>;
  reset: () => Promise<void>;
  reload: () => void;
}

const SettingsContext = createContext<SettingsState | null>(null);

/** The theme actually shown: "system" follows the OS. */
export function effectiveTheme(theme: string): "dark" | "light" {
  if (theme === "dark" || theme === "light") return theme;
  return window.matchMedia?.("(prefers-color-scheme: light)").matches ? "light" : "dark";
}

export function SettingsProvider({ children }: { children: ReactNode }) {
  const [view, setView] = useState<SettingsView | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [tick, setTick] = useState(0);

  useEffect(() => {
    SettingsService.Get()
      .then((v) => {
        setView(v);
        setError(null);
      })
      .catch((err) => setError(errorMessage(err)));
  }, [tick]);

  const settings = view?.settings ?? DEFAULT_SETTINGS;

  // Theme: data-theme on <html> overrides the OS preference.
  useEffect(() => {
    const root = document.documentElement;
    if (settings.theme === "system") delete root.dataset.theme;
    else root.dataset.theme = settings.theme;
  }, [settings.theme]);

  // Interface size: the web view's own zoom, so every page and library scales.
  useEffect(() => {
    try {
      void Promise.resolve(Window.SetZoom(settings.uiScale / 100)).catch(() => {});
    } catch {
      // Not running inside the desktop app (browser testing).
    }
  }, [settings.uiScale]);

  const save = useCallback(
    async (patch: Partial<Settings>) => {
      try {
        const saved = await SettingsService.Save({ ...settings, ...patch });
        setView((v) => (v ? { ...v, settings: saved } : v));
        return null;
      } catch (err) {
        return errorMessage(err);
      }
    },
    [settings],
  );

  const enableTerminal = useCallback(async () => {
    const on = await SettingsService.EnableTerminal();
    setTick((n) => n + 1);
    return on;
  }, []);

  const reset = useCallback(async () => {
    const saved = await SettingsService.Reset();
    setView((v) => (v ? { ...v, settings: saved } : v));
  }, []);

  const value = useMemo<SettingsState>(
    () => ({ settings, view, error, save, enableTerminal, reset, reload: () => setTick((n) => n + 1) }),
    [settings, view, error, save, enableTerminal, reset],
  );
  return <SettingsContext.Provider value={value}>{children}</SettingsContext.Provider>;
}

export function useSettings(): SettingsState {
  const state = useContext(SettingsContext);
  if (!state) throw new Error("useSettings must be used inside SettingsProvider");
  return state;
}
