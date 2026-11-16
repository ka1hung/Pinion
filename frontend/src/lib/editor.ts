// Editor documents: metadata + Monaco models keyed by document ID.
import { EditorService } from "./services";
import { errMsg } from "./api";
import { useStore, toast, type EditorTab } from "./store";
import { confirmDialog } from "../components/ui";

export interface Doc {
  id: string;
  source: "local" | "remote";
  tabId: string;
  path: string;
  name: string;
  content: string;
  encoding: string;
  eol: string;
  mtime: number;
  size: number;
  readOnly: boolean;
  tooLarge: boolean;
}

export const docs = new Map<string, Doc>();

function addEditorTab(d: Doc) {
  docs.set(d.id, d);
  const st = useStore.getState();
  const existing = st.tabs.find((t) => t.kind === "editor" && t.docId === d.id);
  if (existing) return st.activate(existing.id);
  const host = d.source === "remote" ? st.tabs.find((t) => t.id === d.tabId)?.title : undefined;
  const tab: EditorTab = {
    kind: "editor",
    id: "e:" + d.id,
    docId: d.id,
    title: d.name,
    path: host ? `${host}:${d.path}` : d.path,
    source: d.source,
    termTabId: d.source === "remote" ? d.tabId : undefined,
    dirty: false,
  };
  st.addTab(tab);
}

/** Find an already-open editor for the same file. */
function findOpen(source: string, tabId: string, path: string) {
  for (const d of docs.values()) if (d.source === source && d.tabId === tabId && d.path === path) return d;
  return null;
}

async function handleTooLarge(d: Doc, reopen: () => Promise<Doc | null>, external?: () => void) {
  const limit = useStore.getState().settings?.bigFileLimitMB ?? 10;
  const ok = await confirmDialog(
    "檔案過大",
    `「${d.name}」超過 ${limit} MB。\n要以唯讀方式開啟${external ? "，或取消後改用外部編輯器" : ""}嗎？`,
    { ok: "唯讀開啟" },
  );
  if (!ok) {
    external?.();
    return null;
  }
  return reopen();
}

export async function openRemoteFile(tabId: string, path: string, force = false): Promise<void> {
  const st = useStore.getState();
  if (st.settings?.useExternalEditor && !force) return openExternal(tabId, path);
  const prev = findOpen("remote", tabId, path);
  if (prev) {
    const t = st.tabs.find((x) => x.kind === "editor" && x.docId === prev.id);
    if (t) return st.activate(t.id);
  }
  try {
    let d = (await EditorService.OpenRemote(tabId, path, "", force)) as Doc;
    if (d.tooLarge && !force) {
      const r = await handleTooLarge(
        d,
        async () => (await EditorService.OpenRemote(tabId, path, "", true)) as Doc,
        () => openExternal(tabId, path),
      );
      if (!r) return;
      d = r;
    }
    addEditorTab(d);
  } catch (e) {
    toast("error", errMsg(e));
  }
}

export async function openLocalFile(path?: string): Promise<void> {
  try {
    const p = path || (await EditorService.PickLocal());
    if (!p) return;
    const prev = findOpen("local", "", p);
    if (prev) {
      const t = useStore.getState().tabs.find((x) => x.kind === "editor" && x.docId === prev.id);
      if (t) return useStore.getState().activate(t.id);
    }
    let d = (await EditorService.OpenLocal(p, "", false)) as Doc;
    if (d.tooLarge) {
      const r = await handleTooLarge(d, async () => (await EditorService.OpenLocal(p, "", true)) as Doc);
      if (!r) return;
      d = r;
    }
    addEditorTab(d);
  } catch (e) {
    toast("error", errMsg(e));
  }
}

export async function openExternal(tabId: string, path: string) {
  try {
    await EditorService.OpenExternal(tabId, path);
    toast("info", `已用外部編輯器開啟 ${path.split("/").pop()}，存檔會自動上傳`);
  } catch (e) {
    toast("error", errMsg(e));
  }
}

export async function closeEditor(tab: EditorTab): Promise<boolean> {
  if (tab.dirty) {
    const ok = await confirmDialog("尚未儲存", `「${tab.title}」有未儲存的變更，確定要關閉嗎？`, { danger: true, ok: "不儲存並關閉" });
    if (!ok) return false;
  }
  docs.delete(tab.docId);
  window.dispatchEvent(new CustomEvent("pinion:doc-closed", { detail: tab.docId }));
  EditorService.Close(tab.docId).catch(() => {});
  return true;
}
