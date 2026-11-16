// Monaco is bundled with the app (no CDN) so the editor works offline.
import * as monaco from "monaco-editor";
import { loader } from "@monaco-editor/react";
import EditorWorker from "monaco-editor/editor/editor.worker?worker";
import JsonWorker from "monaco-editor/language/json/json.worker?worker";
import CssWorker from "monaco-editor/language/css/css.worker?worker";
import HtmlWorker from "monaco-editor/language/html/html.worker?worker";
import TsWorker from "monaco-editor/language/typescript/ts.worker?worker";

(self as any).MonacoEnvironment = {
  getWorker(_: string, label: string) {
    if (label === "json") return new JsonWorker();
    if (label === "css" || label === "scss" || label === "less") return new CssWorker();
    if (label === "html" || label === "handlebars" || label === "razor") return new HtmlWorker();
    if (label === "typescript" || label === "javascript") return new TsWorker();
    return new EditorWorker();
  },
};

loader.config({ monaco });

// VS Code "Dark Modern" / "Light Modern" approximations.
monaco.editor.defineTheme("dark-modern", {
  base: "vs-dark",
  inherit: true,
  rules: [],
  colors: {
    "editor.background": "#1f1f1f",
    "editor.foreground": "#cccccc",
    "editorLineNumber.foreground": "#6e7681",
    "editorLineNumber.activeForeground": "#cccccc",
    "editor.lineHighlightBorder": "#282828",
    "editorWidget.background": "#202020",
    "editorGutter.background": "#1f1f1f",
  },
});
monaco.editor.defineTheme("dark-dim", {
  base: "vs-dark",
  inherit: true,
  rules: [],
  colors: {
    "editor.background": "#22272e",
    "editor.foreground": "#dde3ea",
    "editorLineNumber.foreground": "#636e7b",
    "editorLineNumber.activeForeground": "#cdd5df",
    "editor.lineHighlightBackground": "#2d333b",
    "editor.selectionBackground": "#3e5573",
    "editorWidget.background": "#2d333b",
    "editorGutter.background": "#22272e",
    "minimap.background": "#22272e",
  },
});
monaco.editor.defineTheme("light-modern", {
  base: "vs",
  inherit: true,
  rules: [],
  colors: {
    "editor.background": "#ffffff",
    "editor.foreground": "#3b3b3b",
    "editorLineNumber.foreground": "#6e7681",
    "editorLineNumber.activeForeground": "#171184",
  },
});

// nginx config
monaco.languages.register({ id: "nginx", extensions: [".nginx"], filenames: ["nginx.conf"], aliases: ["nginx"] });
monaco.languages.setMonarchTokensProvider("nginx", {
  tokenizer: {
    root: [
      [/#.*$/, "comment"],
      [/"([^"\\]|\\.)*"/, "string"],
      [/'([^'\\]|\\.)*'/, "string"],
      [/\$[\w_]+/, "variable"],
      [/\b(on|off)\b/, "keyword"],
      [/^\s*[a-z_]+(?=\s|\{)/, "type.identifier"],
      [/\b\d+[kmgsKMG]?\b/, "number"],
      [/[{};]/, "delimiter"],
    ],
  },
});
monaco.languages.setLanguageConfiguration("nginx", {
  comments: { lineComment: "#" },
  brackets: [["{", "}"]],
  autoClosingPairs: [
    { open: "{", close: "}" },
    { open: '"', close: '"' },
    { open: "'", close: "'" },
  ],
});

// systemd units / generic key=value with [sections]
monaco.languages.register({
  id: "systemd",
  extensions: [".service", ".socket", ".timer", ".mount", ".target", ".path", ".network", ".netdev", ".link"],
  aliases: ["systemd"],
});
monaco.languages.setMonarchTokensProvider("systemd", {
  tokenizer: {
    root: [
      [/^\s*[#;].*$/, "comment"],
      [/^\s*\[[^\]]+\]/, "metatag"],
      [/^\s*[A-Za-z0-9_.-]+(?=\s*=)/, "key"],
      [/=/, "delimiter"],
      [/%[a-zA-Z%]/, "variable"],
      [/\b(yes|no|true|false|on|off)\b/, "keyword"],
      [/\b\d+(s|min|h|ms|us)?\b/, "number"],
    ],
  },
});

const byName: Record<string, string> = {
  dockerfile: "dockerfile",
  makefile: "makefile",
  "nginx.conf": "nginx",
  ".bashrc": "shell",
  ".bash_profile": "shell",
  ".profile": "shell",
  ".zshrc": "shell",
  "crontab": "shell",
  hosts: "ini",
  "sshd_config": "ini",
  "ssh_config": "ini",
};
const byExt: Record<string, string> = {
  sh: "shell", bash: "shell", zsh: "shell", ksh: "shell",
  conf: "ini", cfg: "ini", ini: "ini", properties: "ini", env: "ini", toml: "ini",
  yml: "yaml", yaml: "yaml", json: "json", jsonc: "json",
  xml: "xml", html: "html", htm: "html", css: "css", scss: "scss", less: "less",
  js: "javascript", mjs: "javascript", cjs: "javascript", ts: "typescript", tsx: "typescript", jsx: "javascript",
  py: "python", go: "go", rs: "rust", c: "c", h: "c", cpp: "cpp", hpp: "cpp", cc: "cpp", cs: "csharp",
  java: "java", kt: "kotlin", php: "php", rb: "ruby", pl: "perl", lua: "lua", sql: "sql",
  md: "markdown", ps1: "powershell", psm1: "powershell", bat: "bat", cmd: "bat",
  service: "systemd", socket: "systemd", timer: "systemd", mount: "systemd", target: "systemd",
  log: "plaintext", txt: "plaintext", csv: "plaintext",
};

export function languageFor(path: string): string {
  const name = path.split(/[\\/]/).pop()!.toLowerCase();
  if (byName[name]) return byName[name];
  if (name.startsWith("dockerfile")) return "dockerfile";
  if (/nginx/.test(path) && name.endsWith(".conf")) return "nginx";
  if (path.includes("/systemd/")) return "systemd";
  const ext = name.includes(".") ? name.split(".").pop()! : "";
  return byExt[ext] ?? "plaintext";
}

export const languages = () => monaco.languages.getLanguages().map((l) => l.id).sort();

export { monaco };
