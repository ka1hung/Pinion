// SFTP transfers live in a global store so progress and failures are not lost
// when the SFTP panel is unmounted (e.g. while another tab is active).
import { create } from "zustand";
import { on, errMsg } from "./api";
import { toast } from "./store";
import { t as tr, type TKey } from "./i18n";

export interface Transfer {
  id: string;
  tabId: string;
  kind: string;
  name: string;
  files: number;
  filesDone: number;
  bytes: number;
  totalBytes: number;
  state: string;
  error: string;
}

export const useTransfers = create<{ items: Record<string, Transfer>; dismiss: (id: string) => void; clearFinished: (tabId: string) => void }>(
  (set) => ({
    items: {},
    dismiss: (id) =>
      set((s) => {
        const items = { ...s.items };
        delete items[id];
        return { items };
      }),
    clearFinished: (tabId) =>
      set((s) => ({
        items: Object.fromEntries(Object.entries(s.items).filter(([, t]) => t.tabId !== tabId || t.state === "running")),
      })),
  }),
);

const KINDS = ["upload", "download", "copy", "move"];
/** Localized label for a transfer kind (resolved at call time, so it follows the language setting). */
export const kindLabel = (kind: string) => (KINDS.includes(kind) ? tr(`transfers.kind.${kind}` as TKey) : kind);

/** Remote clipboard for SFTP cut/copy/paste (per SSH tab). */
export const useSftpClip = create<{ tabId: string; mode: "copy" | "move"; paths: string[]; set: (tabId: string, mode: "copy" | "move", paths: string[]) => void; clear: () => void }>(
  (set) => ({
    tabId: "",
    mode: "copy",
    paths: [],
    set: (tabId, mode, paths) => set({ tabId, mode, paths }),
    clear: () => set({ tabId: "", paths: [] }),
  }),
);

let started = false;
export function initTransfers() {
  if (started) return;
  started = true;
  on<Transfer>("sftp:transfer", (t) => {
    const prev = useTransfers.getState().items[t.id];
    if (prev && prev.state !== "running") return; // ignore late progress after the final state
    useTransfers.setState((s) => ({ items: { ...s.items, [t.id]: t } }));
    if (t.state === "running") return;
    if (t.state === "error") toast("error", tr("transfers.failed", { kind: kindLabel(t.kind), error: errMsg(t.error) }));
    // finished entries clear themselves; failures stay a bit longer
    const ttl = t.state === "done" ? 3000 : 10000;
    setTimeout(() => useTransfers.getState().dismiss(t.id), ttl);
  });
}
