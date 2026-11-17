// Thin re-export of the generated Wails bindings plus event helpers.
export * as SessionService from "../../bindings/github.com/ka1hung/pinion/internal/app/sessionservice";
export * as TerminalService from "../../bindings/github.com/ka1hung/pinion/internal/app/terminalservice";
export * as SystemService from "../../bindings/github.com/ka1hung/pinion/internal/app/systemservice";
export type * as store from "../../bindings/github.com/ka1hung/pinion/internal/store/models";
export type * as tabs from "../../bindings/github.com/ka1hung/pinion/internal/tabs/models";
export type * as fonts from "../../bindings/github.com/ka1hung/pinion/internal/fonts/models";

import { Events } from "@wailsio/runtime";
import { t } from "./i18n";

/** Subscribe to a backend event; returns an unsubscribe function. */
export function on<T = any>(name: string, cb: (data: T) => void): () => void {
  return Events.On(name, (ev: any) => cb(ev?.data as T));
}

/** Turn any thrown value from a binding into a readable message. */
export function errMsg(e: unknown): string {
  if (!e) return t("common.unknownError");
  if (typeof e === "string") return e;
  const anyE = e as any;
  if (anyE.message) {
    try {
      const j = JSON.parse(anyE.message);
      if (j?.message) return j.message;
    } catch {
      /* not JSON */
    }
    return anyE.message;
  }
  return String(e);
}

export const isMac = navigator.userAgent.includes("Mac");

/** Mirrors prompt.Request in Go (only emitted as an event, so not in bindings). */
export namespace prompt {
  export interface Field {
    label: string;
    echo: boolean;
  }
  export interface Request {
    id: string;
    kind: string;
    title: string;
    message: string;
    fields?: Field[];
    fingerprint?: string;
    canSave?: boolean;
    tabId?: string;
    /** Values for building localized text (title/message/labels are English fallbacks). */
    params?: Record<string, string>;
  }
}
