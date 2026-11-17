import { create } from "zustand";
import { SystemService, type fonts as F } from "./api";

// Font handling for the terminal and editor.
//
// An empty terminalFont / editorFont setting means "platform default" (the chains
// below). Picking a font in Settings stores `"<font>", <platform default>` so
// CJK and missing glyphs still fall back sensibly.
//
// Webviews (WebView2 / WKWebView / WebKitGTK) can't enumerate system fonts without
// a permission prompt (queryLocalFonts) or at all, so we probe a list of well-known
// monospace families and keep the ones that actually render differently from the
// generic fallbacks.

const isLinux = /Linux/.test(navigator.userAgent) && !/Android/.test(navigator.userAgent);

// Linux: Menlo/Consolas never exist, and the old chain landed on DejaVu Sans Mono,
// which doesn't match the desktop. Prefer Ubuntu's own mono font (24.04+), then
// DejaVu, then whatever fontconfig maps "monospace" to.
const TERMINAL_DEFAULT = isLinux
  ? `"Ubuntu Sans Mono", "DejaVu Sans Mono", "Noto Sans Mono CJK TC", monospace`
  : `Menlo, Consolas, "DejaVu Sans Mono", "Noto Sans Mono CJK TC", monospace`;
const EDITOR_DEFAULT = isLinux
  ? `"Ubuntu Sans Mono", "DejaVu Sans Mono", "Noto Sans Mono CJK TC", monospace`
  : `Menlo, Consolas, "DejaVu Sans Mono", monospace`;

export type FontKind = "terminal" | "editor";

export function defaultFont(kind: FontKind): string {
  return kind === "terminal" ? TERMINAL_DEFAULT : EDITOR_DEFAULT;
}

/** Effective CSS font-family for a stored setting value. */
export function resolveFont(kind: FontKind, value: string | undefined): string {
  return value?.trim() ? value : defaultFont(kind);
}

/** Stored value for a font picked from the list. */
export function fontChain(kind: FontKind, font: string): string {
  return `"${font}", ${defaultFont(kind)}`;
}

/** Which listed font (if any) a stored value corresponds to. */
export function pickedFont(kind: FontKind, value: string, fonts: string[]): string | null {
  return fonts.find((f) => value === fontChain(kind, f)) ?? null;
}

const CANDIDATES = [
  // Windows
  "Cascadia Code",
  "Cascadia Mono",
  "Consolas",
  "Lucida Console",
  "Courier New",
  "MS Gothic",
  "NSimSun",
  "MingLiU",
  // macOS
  "SF Mono",
  "Menlo",
  "Monaco",
  "Andale Mono",
  // Linux
  "Ubuntu Sans Mono",
  "Ubuntu Mono",
  "DejaVu Sans Mono",
  "Liberation Mono",
  "Noto Sans Mono",
  "Noto Sans Mono CJK TC",
  "Droid Sans Mono",
  // Popular installs
  "JetBrains Mono",
  "JetBrainsMono Nerd Font",
  "Fira Code",
  "FiraCode Nerd Font",
  "Fira Mono",
  "Source Code Pro",
  "Hack",
  "Hack Nerd Font",
  "Inconsolata",
  "IBM Plex Mono",
  "Roboto Mono",
  "Iosevka",
  "Victor Mono",
  "Geist Mono",
  "Monaspace Neon",
  "Maple Mono",
  "Maple Mono NF CN",
  "Sarasa Mono TC",
  "Sarasa Term TC",
  "Sarasa Fixed TC",
  "MesloLGS NF",
  "MesloLGS Nerd Font",
  "CaskaydiaCove Nerd Font",
  "Anonymous Pro",
  "Space Mono",
  "Comic Mono",
];

// ---- detection ----

const GENERIC = new Set(["monospace", "serif", "sans-serif", "system-ui", "ui-monospace", "ui-serif", "ui-sans-serif", "cursive", "fantasy", "emoji", "math"]);
const SAMPLE = "mmmmmmmmmmlliI10Oo@#%WwQq中文";
const BASES = ["monospace", "sans-serif", "serif"];
let ctx: CanvasRenderingContext2D | null | undefined;
let baseW: number[] = [];

function width(family: string) {
  ctx!.font = `72px ${family}`;
  return ctx!.measureText(SAMPLE).width;
}

/** Whether a single family name renders with a real font (installed or imported and loaded). */
export function fontAvailable(name: string): boolean {
  if (GENERIC.has(name.toLowerCase())) return true;
  if (ctx === undefined) {
    ctx = document.createElement("canvas").getContext("2d");
    if (ctx) baseW = BASES.map(width);
  }
  if (!ctx) return true; // can't tell; don't claim it's missing
  const q = `"${name.replace(/"/g, '\\"')}"`;
  return BASES.some((b, i) => width(`${q}, ${b}`) !== baseW[i]);
}

/** Family names in a CSS font-family list, unquoted. */
export function parseFamilies(css: string): string[] {
  const out: string[] = [];
  const re = /\s*(?:"([^"]*)"|'([^']*)'|([^,]+))\s*(?:,|$)/g;
  let m: RegExpExecArray | null;
  while ((m = re.exec(css)) && m[0]) {
    const v = (m[1] ?? m[2] ?? m[3] ?? "").trim();
    if (v) out.push(v);
  }
  return out;
}

let cache: string[] | null = null;

/** Installed fonts from CANDIDATES. */
export function installedMonoFonts(): string[] {
  return (cache ??= CANDIDATES.filter(fontAvailable));
}

// ---- fonts imported into Pinion (served from the config dir at /userfonts/) ----

export const useUserFonts = create<{ fonts: F.Font[] }>(() => ({ fonts: [] }));
const faces = new Map<string, FontFace>(); // by file name

async function register(f: F.Font) {
  faces.get(f.file) && document.fonts.delete(faces.get(f.file)!);
  // Cache-bust so a re-imported file with the same name isn't served stale.
  const face = new FontFace(f.family, `url("${f.url}?t=${Date.now()}")`, { weight: f.weight, style: f.style });
  faces.set(f.file, face);
  document.fonts.add(face);
  await face.load();
}

/** Load all imported fonts; call before the UI renders so terminals measure the right glyphs. */
export async function loadUserFonts() {
  const list = ((await SystemService.UserFonts()) ?? []).filter(Boolean) as F.Font[];
  useUserFonts.setState({ fonts: list });
  await Promise.allSettled(list.map(register));
}

/** Show the file dialog and import a font; returns its family, or null if cancelled. */
export async function importFont(title: string): Promise<string | null> {
  const f = await SystemService.ImportFont(title);
  if (!f) return null;
  await register(f).catch(() => {});
  const rest = useUserFonts.getState().fonts.filter((x) => x.file !== f.file);
  useUserFonts.setState({ fonts: [...rest, f].sort((a, b) => a.family.localeCompare(b.family) || a.file.localeCompare(b.file)) });
  return f.family;
}

export async function removeFont(file: string) {
  await SystemService.RemoveFont(file);
  const face = faces.get(file);
  if (face) document.fonts.delete(face);
  faces.delete(file);
  useUserFonts.setState({ fonts: useUserFonts.getState().fonts.filter((x) => x.file !== file) });
}

/** Distinct family names of the imported fonts. */
export function userFamilies(list: F.Font[]): string[] {
  return [...new Set(list.map((f) => f.family))];
}
