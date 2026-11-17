// Terminal instances live outside React so tabs can move between panes
// without losing state. Each tab owns one xterm.js instance and one local
// WebSocket carrying raw bytes (binary frames) to/from the Go backend.
import { Terminal, type ITheme } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { SearchAddon } from "@xterm/addon-search";
import { WebLinksAddon } from "@xterm/addon-web-links";
import { Unicode11Addon } from "@xterm/addon-unicode11";
import { WebglAddon } from "@xterm/addon-webgl";
import { Clipboard, Browser } from "@wailsio/runtime";
import { TerminalService, isMac } from "./api";
import { resolveFont } from "./fonts";
import { useStore, type ViewOpts } from "./store";
import { openMenu } from "../components/ui";
import { t } from "./i18n";

let streamInfo: { port: number; token: string } | null = null;
async function getStreamInfo() {
  if (!streamInfo) streamInfo = await TerminalService.StreamInfo();
  return streamInfo;
}

export const darkTheme: ITheme = {
  background: "#1f1f1f",
  foreground: "#cccccc",
  cursor: "#aeafad",
  selectionBackground: "#264f78",
  black: "#000000",
  red: "#cd3131",
  green: "#0dbc79",
  yellow: "#e5e510",
  blue: "#2472c8",
  magenta: "#bc3fbc",
  cyan: "#11a8cd",
  white: "#e5e5e5",
  brightBlack: "#666666",
  brightRed: "#f14c4c",
  brightGreen: "#23d18b",
  brightYellow: "#f5f543",
  brightBlue: "#3b8eea",
  brightMagenta: "#d670d6",
  brightCyan: "#29b8db",
  brightWhite: "#e5e5e5",
};

/** Default dark terminal: soft slate background, slightly muted ANSI colours. */
export const dimTheme: ITheme = {
  background: "#22272e",
  foreground: "#dde3ea",
  cursor: "#6cb6ff",
  selectionBackground: "#3e5573",
  black: "#545d68",
  red: "#f47067",
  green: "#6bc46d",
  yellow: "#daaa3f",
  blue: "#539bf5",
  magenta: "#b083f0",
  cyan: "#39c5cf",
  white: "#909dab",
  brightBlack: "#768390",
  brightRed: "#ff938a",
  brightGreen: "#8ddb8c",
  brightYellow: "#eac55f",
  brightBlue: "#6cb6ff",
  brightMagenta: "#dcbdfb",
  brightCyan: "#56d4dd",
  brightWhite: "#cdd9e5",
};

export const lightTheme: ITheme = {
  background: "#ffffff",
  foreground: "#3b3b3b",
  cursor: "#005fb8",
  selectionBackground: "#add6ff",
  black: "#000000",
  red: "#cd3131",
  green: "#107c10",
  yellow: "#949800",
  blue: "#0451a5",
  magenta: "#bc05bc",
  cyan: "#0598bc",
  white: "#555555",
  brightBlack: "#666666",
  brightRed: "#cd3131",
  brightGreen: "#14ce14",
  brightYellow: "#b5ba00",
  brightBlue: "#0451a5",
  brightMagenta: "#bc05bc",
  brightCyan: "#0598bc",
  brightWhite: "#a5a5a5",
};

type Activity = { tx: number; rx: number };
type Listener = () => void;

const enc = new TextEncoder();

function pad(n: number, w = 2) {
  return String(n).padStart(w, "0");
}
function stamp() {
  const d = new Date();
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}.${pad(d.getMilliseconds(), 3)}`;
}
const hexDigits = "0123456789ABCDEF";

class TermInstance {
  readonly id: string;
  term: Terminal;
  fit = new FitAddon();
  search = new SearchAddon();
  host: HTMLDivElement;
  ws: WebSocket | null = null;
  disposed = false;
  view: ViewOpts = { hex: false, timestamp: false, json: false };
  activity: Activity = { tx: 0, rx: 0 };
  listeners = new Set<Listener>();
  /** Shell working directory as reported by OSC 7 or parsed from the prompt
   * ("~", "~/app" or an absolute path). Empty until known. */
  cwd = "";
  cwdListeners = new Set<(cwd: string) => void>();
  private promptTimer: number | null = null;
  private lineStart = true;
  private hexCol = 0;
  private jsonBuf = "";
  private jsonTimer: number | null = null;
  private decoder = new TextDecoder();
  private reconnectTimer: number | null = null;
  private connectedOnce = false;
  private resizeObs: ResizeObserver;

  constructor(id: string) {
    this.id = id;
    const st = useStore.getState().settings;
    this.term = new Terminal({
      allowProposedApi: true,
      fontFamily: resolveFont("terminal", st?.terminalFont),
      fontSize: st?.terminalFontSize || 14,
      scrollback: st?.scrollback || 10000,
      cursorBlink: st?.cursorBlink ?? true,
      theme: termTheme(),
      macOptionIsMeta: true,
      rightClickSelectsWord: false,
    });
    this.term.loadAddon(this.fit);
    this.term.loadAddon(this.search);
    this.term.loadAddon(new WebLinksAddon((_e, uri) => Browser.OpenURL(uri)));
    const u11 = new Unicode11Addon();
    this.term.loadAddon(u11);
    this.term.unicode.activeVersion = "11";
    this.host = document.createElement("div");
    this.host.className = "term-host";
    this.host.style.width = "100%";
    this.host.style.height = "100%";
    this.host.style.background = termTheme().background!;
    this.term.open(this.host);
    try {
      const gl = new WebglAddon();
      gl.onContextLoss(() => gl.dispose());
      this.term.loadAddon(gl);
    } catch {
      /* WebGL unavailable: fall back to DOM renderer */
    }
    // OSC 7: file://host/path — emitted by fish, zsh/bash with vte.sh, etc.
    this.term.parser.registerOscHandler(7, (data) => {
      const m = data.match(/^file:\/\/[^/]*(\/.*)$/);
      if (m) {
        try {
          this.setCwd(decodeURIComponent(m[1]));
        } catch {
          this.setCwd(m[1]);
        }
      }
      return true;
    });
    this.term.attachCustomKeyEventHandler((e) => this.keyHandler(e));
    this.term.onData((d) => this.userInput(d));
    this.term.onBinary((d) => this.sendBytes(Uint8Array.from(d, (c) => c.charCodeAt(0))));
    // Right click: context menu, or PuTTY-style copy-or-paste.
    this.host.addEventListener("contextmenu", (ev) => {
      ev.preventDefault();
      if ((useStore.getState().settings?.termRightClick ?? "menu") === "paste") {
        if (!this.copySelection()) this.pasteClipboard();
        else this.term.clearSelection();
        return;
      }
      const has = this.term.hasSelection();
      openMenu(ev, [
        { label: t("common.copy"), disabled: !has, shortcut: isMac ? "⌘C" : "Ctrl+C", onClick: () => this.copySelection() },
        { label: t("common.paste"), shortcut: isMac ? "⌘V" : "Ctrl+V", onClick: () => this.pasteClipboard() },
        { label: t("terminal.selectAll"), onClick: () => this.term.selectAll() },
        { separator: true },
        { label: t("terminal.searchEllipsis"), shortcut: isMac ? "⌘F" : "Ctrl+Shift+F", onClick: () => window.dispatchEvent(new CustomEvent("pinion:term-search", { detail: this.id })) },
        { label: t("terminal.clear"), onClick: () => this.term.clear() },
      ]);
    });
    // Middle click pastes (X11 habit)
    this.host.addEventListener("auxclick", (ev) => {
      if (ev.button === 1) {
        ev.preventDefault();
        this.pasteClipboard();
      }
    });
    this.term.onSelectionChange(() => {
      if (useStore.getState().settings?.copyOnSelect) {
        const s = this.term.getSelection();
        if (s) Clipboard.SetText(s);
      }
    });
    this.term.onResize(({ cols, rows }) => this.sendControl({ t: "resize", c: cols, r: rows }));
    this.resizeObs = new ResizeObserver(() => this.fitNow());
    this.resizeObs.observe(this.host);
    this.connect();
  }

  copySelection(): boolean {
    const s = this.term.getSelection();
    if (!s) return false;
    Clipboard.SetText(s);
    return true;
  }

  pasteClipboard() {
    Clipboard.Text().then((t) => t && this.term.paste(t));
  }

  private keyHandler(e: KeyboardEvent): boolean {
    if (e.type !== "keydown") return true;
    if (e.ctrlKey && e.key === "Tab") return false; // tab switching is handled by the app
    const mod = isMac ? e.metaKey : e.ctrlKey && e.shiftKey;
    const k = e.key.toLowerCase();
    const smart = !isMac && (useStore.getState().settings?.smartCopyPaste ?? true);
    const plainCtrl = e.ctrlKey && !e.shiftKey && !e.altKey && !e.metaKey;
    // Copy: ⌘C / Ctrl+Shift+C / Ctrl+Insert, or Ctrl+C while text is selected (smart)
    if ((mod && k === "c") || (e.ctrlKey && k === "insert") || (smart && plainCtrl && k === "c" && this.term.hasSelection())) {
      this.copySelection();
      if (smart && plainCtrl) this.term.clearSelection();
      e.preventDefault();
      return false;
    }
    // Paste: ⌘V / Ctrl+Shift+V / Shift+Insert, or Ctrl+V (smart)
    if ((mod && k === "v") || (e.shiftKey && k === "insert") || (smart && plainCtrl && k === "v")) {
      this.pasteClipboard();
      e.preventDefault();
      return false;
    }
    if (mod && k === "f") {
      window.dispatchEvent(new CustomEvent("pinion:term-search", { detail: this.id }));
      e.preventDefault();
      return false;
    }
    // Let app-level shortcuts through (Cmd/Ctrl+Shift+...)
    if ((isMac && e.metaKey) || (e.ctrlKey && e.shiftKey)) return false;
    return true;
  }

  fitNow() {
    if (!this.host.isConnected || this.host.clientWidth === 0) return;
    try {
      this.fit.fit();
    } catch {
      /* ignore */
    }
  }

  async connect() {
    if (this.disposed) return;
    const { port, token } = await getStreamInfo();
    const ws = new WebSocket(`ws://127.0.0.1:${port}/ws/${encodeURIComponent(this.id)}?token=${token}`);
    ws.binaryType = "arraybuffer";
    this.ws = ws;
    ws.onopen = () => {
      if (this.connectedOnce) {
        // the server replays recent output on re-attach
        this.term.reset();
        this.lineStart = true;
        this.hexCol = 0;
      }
      this.connectedOnce = true;
      this.sendControl({ t: "resize", c: this.term.cols, r: this.term.rows });
    };
    ws.onmessage = (ev) => this.receive(new Uint8Array(ev.data as ArrayBuffer));
    ws.onclose = () => {
      if (this.disposed) return;
      this.ws = null;
      this.reconnectTimer = window.setTimeout(() => this.connect(), 800);
    };
  }

  sendControl(m: object) {
    if (this.ws?.readyState === WebSocket.OPEN) this.ws.send(JSON.stringify(m));
  }

  sendBytes(b: Uint8Array) {
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(b);
      this.bump("tx");
    }
  }

  sendText(s: string) {
    this.sendBytes(enc.encode(s));
  }

  private userInput(d: string) {
    this.sendText(d);
    const m = useStore.getState().multi;
    if (m.on && m.mirror && m.selected.includes(this.id)) {
      for (const id of m.selected) if (id !== this.id) terminals.get(id)?.sendText(d);
    }
  }

  private bump(k: keyof Activity) {
    this.activity[k] = performance.now();
    this.listeners.forEach((l) => l());
  }

  private setCwd(dir: string) {
    if (!dir || dir === this.cwd) return;
    this.cwd = dir;
    this.cwdListeners.forEach((l) => l(dir));
  }

  /** Fallback when the shell does not send OSC 7: read the prompt on the
   * cursor line, e.g. "root@NanoPi-NEO:~/app# " or "user@host:/etc$ ". */
  private checkPrompt() {
    const buf = this.term.buffer.active;
    const line = buf.getLine(buf.baseY + buf.cursorY)?.translateToString(true) ?? "";
    const m = line.slice(0, buf.cursorX).match(/[\w.-]+@[\w.-]+:\s*(~[^\s#$%>]*|\/[^\s#$%>]*)\s*[#$%>]\s*$/);
    if (m) this.setCwd(m[1]);
  }

  /** Output from the backend, transformed for HEX / timestamp / JSON views. */
  private receive(b: Uint8Array) {
    this.bump("rx");
    if (this.cwdListeners.size) {
      if (this.promptTimer) clearTimeout(this.promptTimer);
      this.promptTimer = window.setTimeout(() => this.checkPrompt(), 120);
    }
    const v = this.view;
    if (v.hex) return this.term.write(this.toHex(b));
    if (v.json) return this.feedJSON(b);
    if (v.timestamp) return this.term.write(this.withStamps(b));
    this.term.write(b);
  }

  private withStamps(b: Uint8Array): string {
    const text = this.decoder.decode(b, { stream: true });
    let out = "";
    for (const ch of text) {
      if (this.lineStart && ch !== "\r") {
        out += `\x1b[90m[${stamp()}]\x1b[0m `;
        this.lineStart = false;
      }
      out += ch;
      if (ch === "\n") this.lineStart = true;
    }
    return out;
  }

  private toHex(b: Uint8Array): string {
    let out = "";
    for (const byte of b) {
      if (this.hexCol === 0 && this.view.timestamp) out += `\x1b[90m[${stamp()}]\x1b[0m `;
      out += hexDigits[byte >> 4] + hexDigits[byte & 15] + " ";
      this.hexCol++;
      if (this.hexCol >= 16 || (byte === 0x0a && this.view.timestamp)) {
        out += "\r\n";
        this.hexCol = 0;
      }
    }
    return out;
  }

  private feedJSON(b: Uint8Array) {
    this.jsonBuf += this.decoder.decode(b, { stream: true });
    const flushLine = (line: string) => {
      const t = line.trim();
      if (!t) return;
      const prefix = this.view.timestamp ? `\x1b[90m[${stamp()}]\x1b[0m ` : "";
      try {
        const pretty = JSON.stringify(JSON.parse(t), null, 2);
        this.term.write(prefix + colorJSON(pretty).replace(/\n/g, "\r\n") + "\r\n");
      } catch {
        this.term.write(prefix + t.replace(/\n/g, "\r\n") + "\r\n");
      }
    };
    // complete documents: newline-delimited or whole buffer parses
    let idx: number;
    while ((idx = this.jsonBuf.indexOf("\n")) >= 0) {
      flushLine(this.jsonBuf.slice(0, idx));
      this.jsonBuf = this.jsonBuf.slice(idx + 1);
    }
    if (this.jsonTimer) clearTimeout(this.jsonTimer);
    this.jsonTimer = window.setTimeout(() => {
      if (this.jsonBuf) {
        flushLine(this.jsonBuf);
        this.jsonBuf = "";
      }
    }, 60);
  }

  setView(v: ViewOpts) {
    if (v.hex !== this.view.hex) {
      this.term.write(v.hex ? "\r\n\x1b[90m--- HEX ---\x1b[0m\r\n" : "\r\n\x1b[90m--- TEXT ---\x1b[0m\r\n");
      this.hexCol = 0;
      this.lineStart = true;
    }
    this.view = { ...v };
  }

  notice(text: string) {
    this.term.write(`\r\n\x1b[36m[${stamp()}] ${text}\x1b[0m\r\n`);
    this.lineStart = true;
    this.hexCol = 0;
  }

  applySettings() {
    const st = useStore.getState().settings;
    if (!st) return;
    this.term.options.fontFamily = resolveFont("terminal", st.terminalFont);
    this.term.options.fontSize = st.terminalFontSize;
    this.term.options.scrollback = st.scrollback;
    this.term.options.cursorBlink = st.cursorBlink;
    const theme = termTheme();
    this.term.options.theme = theme;
    this.host.style.background = theme.background!;
    this.fitNow();
  }

  attach(el: HTMLElement) {
    if (this.host.parentElement !== el) el.appendChild(this.host);
    requestAnimationFrame(() => this.fitNow());
  }

  focus() {
    this.term.focus();
  }

  dispose() {
    this.disposed = true;
    if (this.reconnectTimer) clearTimeout(this.reconnectTimer);
    this.resizeObs.disconnect();
    this.ws?.close();
    this.term.dispose();
    this.host.remove();
  }
}

function colorJSON(s: string): string {
  return s.replace(
    /("(?:\\.|[^"\\])*"\s*:)|("(?:\\.|[^"\\])*")|\b(true|false|null)\b|(-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?)/g,
    (m, key, str, lit, num) => {
      if (key) return `\x1b[94m${key}\x1b[0m`;
      if (str) return `\x1b[32m${str}\x1b[0m`;
      if (lit) return `\x1b[35m${m}\x1b[0m`;
      if (num) return `\x1b[33m${m}\x1b[0m`;
      return m;
    },
  );
}

export function currentTheme(): "dark" | "light" {
  const t = useStore.getState().settings?.theme || "system";
  if (t === "light") return "light";
  if (t === "dark" || t === "dark-modern") return "dark";
  return window.matchMedia("(prefers-color-scheme: light)").matches ? "light" : "dark";
}

/** "light" | "dim" (soft dark, default) | "modern" (VS Code near-black). */
export function themeVariant(): "light" | "dim" | "modern" {
  if (currentTheme() === "light") return "light";
  return useStore.getState().settings?.theme === "dark-modern" ? "modern" : "dim";
}

export function termTheme(): ITheme {
  const v = themeVariant();
  return v === "light" ? lightTheme : v === "modern" ? darkTheme : dimTheme;
}

class Registry {
  private map = new Map<string, TermInstance>();
  get(id: string) {
    return this.map.get(id);
  }
  ensure(id: string) {
    let t = this.map.get(id);
    if (!t) {
      t = new TermInstance(id);
      this.map.set(id, t);
    }
    return t;
  }
  dispose(id: string) {
    this.map.get(id)?.dispose();
    this.map.delete(id);
  }
  all() {
    return [...this.map.values()];
  }
}

export const terminals = new Registry();
export type { TermInstance };
