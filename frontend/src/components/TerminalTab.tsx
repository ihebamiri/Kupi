import { useEffect, useImperativeHandle, useRef, type Ref } from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { SearchAddon } from "@xterm/addon-search";
import { WebLinksAddon } from "@xterm/addon-web-links";
import "@xterm/xterm/css/xterm.css";
import { Clipboard } from "@wailsio/runtime";
import { KupiTerminal, TerminalService, errorMessage, type Settings, type TerminalChunk, type TerminalSession } from "../lib/api";
import { bufferedListener } from "../lib/events";

/** What the page can do with a tab. */
export interface TerminalApi {
  /** Types text at the prompt without pressing Enter. */
  insert: (text: string) => void;
  /** Pastes text that already passed the paste check. */
  paste: (text: string) => void;
  findNext: (q: string) => boolean;
  findPrevious: (q: string) => boolean;
  clearSearch: () => void;
  /** Everything in the scrollback as plain text. */
  text: () => string;
  clear: () => void;
  focus: () => void;
}

export interface TabSpec {
  key: string;
  shellId: string;
  context: string;
  namespace: string;
}

interface TerminalTabProps {
  spec: TabSpec;
  visible: boolean;
  settings: Settings;
  apiRef: Ref<TerminalApi>;
  onSession: (info: TerminalSession | null) => void;
  onExit: (code: number) => void;
  /** A paste that needs a look first; the page asks, then calls paste(). */
  onPaste: (text: string) => void;
  onLink: (uri: string) => void;
  onError: (message: string) => void;
}

// A dark palette that matches Kupi's navy theme, with readable ANSI colors.
const THEME = {
  background: "#0a0f1e",
  foreground: "#e3e8f4",
  cursor: "#22c3e6",
  cursorAccent: "#0a0f1e",
  selectionBackground: "#2b3f7a",
  black: "#1b2236",
  red: "#ff6b7a",
  green: "#3ddc97",
  yellow: "#f5c04a",
  blue: "#5b8cff",
  magenta: "#c38bff",
  cyan: "#22c3e6",
  white: "#d5dbea",
  brightBlack: "#5d6a8a",
  brightRed: "#ff8f9b",
  brightGreen: "#6ff0b8",
  brightYellow: "#ffd677",
  brightBlue: "#8aaeff",
  brightMagenta: "#d8b0ff",
  brightCyan: "#6fdcf3",
  brightWhite: "#ffffff",
};

const FONT = 'ui-monospace, "Cascadia Mono", "Cascadia Code", "JetBrains Mono", "Ubuntu Mono", Consolas, monospace';

async function copyText(text: string) {
  try {
    await navigator.clipboard.writeText(text);
  } catch {
    await Clipboard.SetText(text);
  }
}

/** One shell in xterm.js. It starts its shell on mount and keeps it while hidden. */
export function TerminalTab({ spec, visible, settings, apiRef, onSession, onExit, onPaste, onLink, onError }: TerminalTabProps) {
  const host = useRef<HTMLDivElement>(null);
  const term = useRef<Terminal | null>(null);
  const fit = useRef<FitAddon | null>(null);
  const search = useRef<SearchAddon | null>(null);
  const sessionId = useRef("");
  // Latest callbacks and settings, read from long-lived listeners.
  const latest = useRef({ settings, onSession, onExit, onPaste, onLink, onError });
  latest.current = { settings, onSession, onExit, onPaste, onLink, onError };

  useImperativeHandle(apiRef, () => ({
    insert: (text) => {
      term.current?.paste(text);
      term.current?.focus();
    },
    paste: (text) => {
      term.current?.paste(text);
      term.current?.focus();
    },
    findNext: (q) => search.current?.findNext(q, { decorations: SEARCH_DECORATIONS }) ?? false,
    findPrevious: (q) => search.current?.findPrevious(q, { decorations: SEARCH_DECORATIONS }) ?? false,
    clearSearch: () => search.current?.clearDecorations(),
    text: () => {
      const t = term.current;
      if (!t) return "";
      const buf = t.buffer.active;
      const lines: string[] = [];
      for (let i = 0; i < buf.length; i++) lines.push(buf.getLine(i)?.translateToString(true) ?? "");
      return lines.join("\n").replace(/\n+$/, "\n");
    },
    clear: () => term.current?.clear(),
    focus: () => term.current?.focus(),
  }));

  useEffect(() => {
    const el = host.current;
    if (!el) return;
    const s = latest.current.settings;
    const t = new Terminal({
      allowProposedApi: true,
      cursorBlink: true,
      fontFamily: FONT,
      fontSize: s.terminalFontSize,
      lineHeight: 1.15,
      scrollback: s.terminalScrollback,
      theme: THEME,
      drawBoldTextInBrightColors: true,
    });
    const f = new FitAddon();
    const sa = new SearchAddon();
    t.loadAddon(f);
    t.loadAddon(sa);
    t.loadAddon(new WebLinksAddon((_event, uri) => latest.current.onLink(uri)));
    t.open(el);
    term.current = t;
    fit.current = f;
    search.current = sa;
    if (el.offsetWidth > 0) f.fit();

    let disposed = false;
    let exited = false;
    let stopListening = () => {};

    const start = async () => {
      exited = false;
      const listener = bufferedListener<TerminalChunk>(KupiTerminal, (chunk) => {
        if (chunk.data) t.write(chunk.data);
        if (chunk.exited) {
          exited = true;
          sessionId.current = "";
          latest.current.onSession(null);
          latest.current.onExit(chunk.code);
          t.write(`\r\n\x1b[2m[The shell ended${chunk.code ? ` with code ${chunk.code}` : ""}. Press Enter to start a new one.]\x1b[0m\r\n`);
        }
      });
      stopListening = listener.off;
      try {
        const info = await TerminalService.Open(spec.shellId, spec.context, spec.namespace, t.cols, t.rows);
        if (disposed) {
          void TerminalService.Close(info.id);
          listener.off();
          return;
        }
        sessionId.current = info.id;
        listener.bind(info.id);
        latest.current.onSession(info);
        t.focus();
      } catch (err) {
        listener.off();
        exited = true;
        latest.current.onError(errorMessage(err));
        t.write(`\x1b[31m${errorMessage(err)}\x1b[0m\r\n\x1b[2m[Press Enter to try again.]\x1b[0m\r\n`);
      }
    };

    const input = t.onData((data) => {
      if (exited) {
        if (data.includes("\r")) {
          stopListening();
          void start();
        }
        return;
      }
      if (sessionId.current) void TerminalService.Input(sessionId.current, data).catch(() => {});
    });
    const resize = t.onResize(({ cols, rows }) => {
      if (sessionId.current) void TerminalService.Resize(sessionId.current, cols, rows);
    });
    const selection = t.onSelectionChange(() => {
      if (latest.current.settings.terminalCopyOnSelect && t.hasSelection()) void copyText(t.getSelection());
    });

    t.attachCustomKeyEventHandler((ev) => {
      if (ev.type !== "keydown") return true;
      const ctrl = ev.ctrlKey || ev.metaKey;
      if (ctrl && ev.shiftKey && ev.code === "KeyC") {
        if (t.hasSelection()) void copyText(t.getSelection());
        return false;
      }
      // Ctrl+C copies when text is selected, like Windows Terminal; otherwise it interrupts.
      if (ctrl && !ev.shiftKey && ev.code === "KeyC" && t.hasSelection()) {
        void copyText(t.getSelection());
        t.clearSelection();
        return false;
      }
      // Let the browser paste, so the paste check sees it.
      if (ctrl && ev.code === "KeyV") return false;
      return true;
    });

    // Every paste goes through the check first.
    const onPasteEvent = (ev: ClipboardEvent) => {
      const text = ev.clipboardData?.getData("text/plain") ?? "";
      ev.preventDefault();
      ev.stopPropagation();
      if (text) latest.current.onPaste(text);
    };
    el.addEventListener("paste", onPasteEvent, true);

    const observer = new ResizeObserver(() => {
      if (el.offsetWidth > 0 && el.offsetHeight > 0) f.fit();
    });
    observer.observe(el);

    void start();

    return () => {
      disposed = true;
      observer.disconnect();
      el.removeEventListener("paste", onPasteEvent, true);
      input.dispose();
      resize.dispose();
      selection.dispose();
      stopListening();
      if (sessionId.current) void TerminalService.Close(sessionId.current);
      t.dispose();
      term.current = null;
    };
    // The tab's shell and cluster are fixed for its life.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Settings that apply to an open terminal.
  useEffect(() => {
    const t = term.current;
    if (!t) return;
    t.options.fontSize = settings.terminalFontSize;
    t.options.scrollback = settings.terminalScrollback;
    if (host.current && host.current.offsetWidth > 0) fit.current?.fit();
  }, [settings.terminalFontSize, settings.terminalScrollback]);

  useEffect(() => {
    if (!visible) return;
    const id = window.requestAnimationFrame(() => {
      if (host.current && host.current.offsetWidth > 0) fit.current?.fit();
      term.current?.focus();
    });
    return () => window.cancelAnimationFrame(id);
  }, [visible]);

  return <div ref={host} className={`kupi-terminal h-full w-full ${visible ? "" : "hidden"}`} />;
}

const SEARCH_DECORATIONS = {
  matchBackground: "#2b3f7a",
  matchOverviewRuler: "#5b8cff",
  activeMatchBackground: "#22c3e6",
  activeMatchColorOverviewRuler: "#22c3e6",
};
