// Tiny i18n: flat key → string dictionaries in src/i18n, English + Traditional
// Chinese (Taiwan). Components call `const t = useT()` so they re-render when
// the language setting changes; non-React code can call `t()` directly.
import { useStore } from "./store";
import { zh, en } from "../i18n";

export type Lang = "zh-TW" | "en";
export type TKey = keyof typeof zh;
export type TParams = Record<string, string | number>;

/** Resolve the "language" setting ("system" | "zh-TW" | "en") to a concrete language. */
export function resolveLang(pref?: string): Lang {
  if (pref === "zh-TW" || pref === "en") return pref;
  const l = (navigator.languages?.[0] || navigator.language || "").toLowerCase();
  return l.startsWith("zh") ? "zh-TW" : "en";
}

export function currentLang(): Lang {
  return resolveLang(useStore.getState().settings?.language);
}

/** Translate key; `{name}` placeholders are filled from params. */
export function t(key: TKey, params?: TParams): string {
  const d: Record<string, string> = currentLang() === "en" ? en : zh;
  let s = d[key] ?? (zh as Record<string, string>)[key] ?? key;
  if (params) s = s.replace(/\{(\w+)\}/g, (m, k) => (k in params ? String(params[k]) : m));
  return s;
}

/** React hook: returns t() and re-renders the component when the language changes. */
export function useT(): typeof t {
  useStore((s) => s.settings?.language);
  return t;
}
