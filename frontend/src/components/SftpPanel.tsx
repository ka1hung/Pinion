import { useCallback, useEffect, useRef, useState } from "react";
import {
  ArrowUp, RefreshCw, Home, FolderPlus, FilePlus, Upload, Download, Trash2, Pencil, Folder, File, Link2,
  ExternalLink, FileCode, KeyRound, Copy, X, Keyboard, Scissors, ClipboardPaste,
} from "lucide-react";
import { Clipboard } from "@wailsio/runtime";
import { SFTPService } from "../lib/services";
import { on, errMsg, SystemService } from "../lib/api";
import { toast, useStore, type TermTab } from "../lib/store";
import { terminals } from "../lib/terminals";
import PermDialog from "./dialogs/PermDialog";
import { useTransfers, useSftpClip } from "../lib/transfers";
import { openRemoteFile, openExternal } from "../lib/editor";
import { openMenu, confirmDialog, inputDialog, fmtBytes, fmtTime, Check } from "./ui";
import { useT } from "../lib/i18n";

interface Entry {
  name: string;
  path: string;
  size: number;
  mode: string;
  perm: number;
  isDir: boolean;
  isLink: boolean;
  linkTarget: string;
  mtime: number;
  owner?: string;
  group?: string;
}

const join = (dir: string, name: string) => (dir.endsWith("/") ? dir + name : dir + "/" + name);
const parent = (p: string) => {
  if (p === "/" || !p) return "/";
  const i = p.replace(/\/+$/, "").lastIndexOf("/");
  return i <= 0 ? "/" : p.slice(0, i);
};

/** "11-16 07:12" this year, "2019-12-19" otherwise. */
function fmtShort(ms: number) {
  if (!ms) return "";
  const d = new Date(ms);
  const p = (x: number) => String(x).padStart(2, "0");
  return d.getFullYear() === new Date().getFullYear()
    ? `${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
    : `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`;
}

const UP = "\u0000up"; // the ".." row

// last folder / home per tab, so switching tabs returns to the same place
const lastDir = new Map<string, string>();
const homeDir = new Map<string, string>();

/** Turn "~", "~/x" from the shell prompt into an absolute path. */
function resolveCwd(cwd: string, home: string) {
  if (!cwd) return "";
  if (cwd === "~") return home;
  if (cwd.startsWith("~/")) return home ? join(home, cwd.slice(2)) : "";
  return cwd.startsWith("/") ? cwd : "";
}

export default function SftpPanel({ tab }: { tab: TermTab }) {
  const tr = useT(); // `t` is used below for transfer items
  const [dir, setDir] = useState(() => lastDir.get(tab.id) ?? "");
  const [perm, setPerm] = useState<Entry | null>(null);
  const follow = useStore((s) => s.settings?.sftpFollowTerminal ?? true);
  const [pathInput, setPathInput] = useState("");
  const [entries, setEntries] = useState<Entry[]>([]);
  const [sel, setSel] = useState<Set<string>>(new Set());
  const [cursor, setCursor] = useState(""); // keyboard focus row (path, or UP)
  const [dropDir, setDropDir] = useState("");
  const listRef = useRef<HTMLDivElement>(null);
  const typeAhead = useRef({ text: "", at: 0 });
  const [err, setErr] = useState("");
  const [loading, setLoading] = useState(false);
  const [showHidden, setShowHidden] = useState(false);
  const [dropHover, setDropHover] = useState(false);
  // dragenter/dragleave fire for every child the pointer crosses, so count
  // them; the outline is only for files dragged in from outside, not rows
  // dragged within (or out of) the list.
  const dropDepth = useRef(0);
  const isInternalDrag = (e: React.DragEvent) => e.dataTransfer.types.includes("pinion/sftp");
  useEffect(() => {
    if (!dropHover) return;
    // Safety net: a drag that ends anywhere (drop elsewhere, Esc) clears it.
    const reset = () => {
      dropDepth.current = 0;
      setDropHover(false);
    };
    window.addEventListener("drop", reset, true);
    window.addEventListener("dragend", reset, true);
    return () => {
      window.removeEventListener("drop", reset, true);
      window.removeEventListener("dragend", reset, true);
    };
  }, [dropHover]);
  const dirRef = useRef(dir);
  dirRef.current = dir;

  const load = useCallback(
    async (d: string, focusName?: string) => {
      setLoading(true);
      try {
        const list = ((await SFTPService.List(tab.id, d)) ?? []) as Entry[];
        setEntries(list.filter(Boolean));
        setDir(d);
        lastDir.set(tab.id, d);
        setPathInput(d);
        setSel(new Set());
        setCursor(focusName ? join(d, focusName) : d !== "/" ? UP : list[0]?.path ?? "");
        setErr("");
      } catch (e) {
        setErr(errMsg(e));
      } finally {
        setLoading(false);
      }
    },
    [tab.id],
  );

  // initial load once connected
  useEffect(() => {
    if (tab.state !== "connected") return;
    if (dirRef.current) {
      load(dirRef.current);
      return;
    }
    SFTPService.Home(tab.id)
      .then((h) => {
        homeDir.set(tab.id, h);
        const c = follow ? resolveCwd(terminals.get(tab.id)?.cwd ?? "", h) : "";
        load(c || h || "/");
      })
      .catch((e) => setErr(errMsg(e)));
  }, [tab.state, tab.id, load]);

  // follow the shell's working directory (OSC 7 or prompt "user@host:~/dir#")
  useEffect(() => {
    const inst = terminals.get(tab.id);
    if (!follow || !inst || tab.state !== "connected") return;
    const go = (cwd: string) => {
      const d = resolveCwd(cwd, homeDir.get(tab.id) ?? "");
      if (d && d !== dirRef.current) load(d);
    };
    inst.cwdListeners.add(go);
    if (inst.cwd) go(inst.cwd);
    return () => {
      inst.cwdListeners.delete(go);
    };
  }, [follow, tab.id, tab.state, load]);

  useEffect(() => {
    const offs = [
      on<{ tabId: string; dir: string }>("sftp:changed", (c) => {
        if (c.tabId === tab.id && (!c.dir || c.dir === dirRef.current)) load(dirRef.current);
      }),
      on<{ files: string[]; target?: { id: string } }>("files:dropped", (d) => {
        dropDepth.current = 0;
        setDropHover(false);
        if (d.target?.id !== "sftp-drop-" + tab.id || !d.files?.length) return;
        SFTPService.Upload(tab.id, d.files, dirRef.current).catch((e) => toast("error", errMsg(e)));
      }),
    ];
    return () => offs.forEach((f) => f());
  }, [tab.id, load]);

  const run = async (p: Promise<any>) => {
    try {
      await p;
    } catch (e) {
      toast("error", errMsg(e));
    }
    load(dirRef.current);
  };

  const visible = entries.filter((e) => showHidden || !e.name.startsWith("."));
  const selected = visible.filter((e) => sel.has(e.path));
  const goUp = () => {
    if (dir && dir !== "/") load(parent(dir), dir.replace(/\/+$/, "").split("/").pop());
  };
  const rows: (Entry | null)[] = dir && dir !== "/" ? [null, ...visible] : visible; // null = ".."
  const rowKey = (e: Entry | null) => (e ? e.path : UP);

  useEffect(() => {
    listRef.current?.querySelector(`[data-key="${CSS.escape(cursor)}"]`)?.scrollIntoView({ block: "nearest" });
  }, [cursor]);

  const onListKey = (ev: React.KeyboardEvent) => {
    const k = ev.key;
    const ctrl = ev.ctrlKey || ev.metaKey;
    let idx = rows.findIndex((r) => rowKey(r) === cursor);
    const moveTo = (i: number) => {
      if (!rows.length) return;
      i = Math.max(0, Math.min(rows.length - 1, i));
      const r = rows[i];
      setCursor(rowKey(r));
      if (!r) return setSel(new Set());
      if (ev.shiftKey && idx >= 0) {
        const s2 = new Set(sel);
        const cur = rows[idx];
        if (cur) s2.add(cur.path);
        s2.add(r.path);
        setSel(s2);
      } else setSel(new Set([r.path]));
    };
    const cur = idx >= 0 ? rows[idx] : undefined;
    const handled = () => {
      ev.preventDefault();
      ev.stopPropagation();
    };
    if (k === "ArrowDown") return handled(), moveTo(idx + 1);
    if (k === "ArrowUp" && ev.altKey) return handled(), goUp();
    if (k === "ArrowUp") return handled(), moveTo(idx < 0 ? 0 : idx - 1);
    if (k === "Home") return handled(), moveTo(0);
    if (k === "End") return handled(), moveTo(rows.length - 1);
    if (k === "PageDown") return handled(), moveTo(idx + 15);
    if (k === "PageUp") return handled(), moveTo(idx - 15);
    if (k === "Enter") return handled(), cur === null ? goUp() : cur && openEntry(cur);
    if (k === "Backspace") return handled(), goUp();
    if (k === "Delete") return handled(), remove(selected.length ? selected : cur ? [cur] : []);
    if (k === "F2" && cur) return handled(), rename(cur);
    if (k === "F5" || (ctrl && k.toLowerCase() === "r")) return handled(), load(dir);
    if (k === "F7" || (ctrl && ev.shiftKey && k.toLowerCase() === "n")) return handled(), newFolder();
    if (ctrl && k.toLowerCase() === "a") return handled(), setSel(new Set(visible.map((x) => x.path)));
    if (ctrl && k.toLowerCase() === "d") return handled(), download(selected.length ? selected : cur ? [cur] : []);
    if (ctrl && k.toLowerCase() === "u") return handled(), SFTPService.UploadDialog(tab.id, dir).catch((e) => toast("error", errMsg(e)));
    const targets = selected.length ? selected : cur ? [cur] : [];
    if (ctrl && ev.shiftKey && k.toLowerCase() === "c") {
      if (targets.length) Clipboard.SetText(targets.map((x) => x.path).join("\n"));
      return handled();
    }
    if (ctrl && k.toLowerCase() === "c") return handled(), toClip("copy", targets);
    if (ctrl && k.toLowerCase() === "x") return handled(), toClip("move", targets);
    if (ctrl && k.toLowerCase() === "v") return handled(), pasteClip(dir);
    if (k === "Escape") {
      handled();
      return terminals.get(tab.id)?.focus();
    }
    // type-ahead: jump to the next name starting with the typed letters
    if (k.length === 1 && !ctrl && !ev.altKey) {
      handled();
      const now = Date.now();
      const ta = typeAhead.current;
      ta.text = now - ta.at > 800 ? k.toLowerCase() : ta.text + k.toLowerCase();
      ta.at = now;
      const start = ta.text.length === 1 ? idx + 1 : Math.max(idx, 0);
      for (let n = 0; n < rows.length; n++) {
        const r = rows[(start + n) % rows.length];
        if (r && r.name.toLowerCase().startsWith(ta.text)) {
          setCursor(r.path);
          setSel(new Set([r.path]));
          break;
        }
      }
    }
  };

  const openEntry = (e: Entry) => {
    if (e.isDir) load(e.path);
    else openRemoteFile(tab.id, e.path);
  };

  const rename = async (e: Entry) => {
    const n = await inputDialog(tr("common.rename"), e.name);
    if (n && n !== e.name) run(SFTPService.Rename(tab.id, e.path, join(parent(e.path), n)));
  };
  const remove = async (list: Entry[]) => {
    if (!list.length) return;
    const names = list.map((x) => x.name).slice(0, 8).join("\n") + (list.length > 8 ? "\n" + tr("sftp.moreItems", { count: list.length }) : "");
    if (await confirmDialog(tr("sftp.deleteTitle"), tr("sftp.deleteConfirm", { names }), { danger: true, ok: tr("common.delete") }))
      run(SFTPService.Remove(tab.id, list.map((x) => x.path)));
  };
  const chmod = (e: Entry) => setPerm(e);
  const clip = useSftpClip();
  const clipFor = clip.tabId === tab.id && clip.paths.length ? clip : null;
  /** Drop dragged rows onto a folder: move, or copy with Ctrl/Alt held. */
  const dropOn = (ev: React.DragEvent, destDir: string) => {
    setDropDir("");
    const raw = ev.dataTransfer.getData("pinion/sftp");
    if (!raw) return;
    ev.preventDefault();
    ev.stopPropagation();
    const d = JSON.parse(raw) as { tabId: string; paths: string[] };
    if (d.tabId !== tab.id) return toast("error", tr("sftp.dragSameConnection"));
    const paths = d.paths.filter((p) => p !== destDir);
    pasteInto(ev.ctrlKey || ev.altKey ? "copy" : "move", paths, destDir);
  };
  const toClip = (mode: "copy" | "move", list: Entry[]) => {
    if (!list.length) return;
    clip.set(tab.id, mode, list.map((x) => x.path));
    toast("info", tr(mode === "move" ? "sftp.cutDone" : "sftp.copyDone", { count: list.length }));
  };
  /** Copy/move remote paths into destDir (asks before overwriting). */
  const pasteInto = async (mode: "copy" | "move", paths: string[], destDir: string) => {
    if (!paths.length) return;
    try {
      const conflicts = ((await SFTPService.PasteConflicts(tab.id, mode, paths, destDir)) ?? []) as string[];
      let overwrite = false;
      if (conflicts.length) {
        const names = conflicts.slice(0, 8).join("\n") + (conflicts.length > 8 ? "\n" + tr("sftp.moreItems", { count: conflicts.length }) : "");
        if (!(await confirmDialog(tr("sftp.existsTitle"), tr("sftp.existsConfirm", { dir: destDir, names }), { danger: true, ok: tr("sftp.overwrite") }))) return;
        overwrite = true;
      }
      await SFTPService.Paste(tab.id, mode, paths, destDir, overwrite);
      if (mode === "move") useSftpClip.getState().clear();
    } catch (e) {
      toast("error", errMsg(e));
    }
  };
  const pasteClip = (destDir: string) => clipFor && pasteInto(clipFor.mode, clipFor.paths, destDir);

  const newFolder = async () => {
    const n = await inputDialog(tr("sftp.newFolder"), "");
    if (n) run(SFTPService.Mkdir(tab.id, join(dirRef.current, n)));
  };
  const download = (list: Entry[]) =>
    list.length && SFTPService.Download(tab.id, list.map((x) => x.path)).catch((er) => toast("error", errMsg(er)));

  const rowMenu = (ev: React.MouseEvent, e: Entry) => {
    if (!sel.has(e.path)) setSel(new Set([e.path]));
    const list = sel.has(e.path) ? selected : [e];
    openMenu(ev, [
      ...(e.isDir
        ? [{ label: tr("common.open"), icon: <Folder size={14} />, onClick: () => load(e.path) }]
        : [
            { label: tr("sftp.openInternal"), icon: <FileCode size={14} />, onClick: () => openRemoteFile(tab.id, e.path, true) },
            { label: tr("sftp.openExternal"), icon: <ExternalLink size={14} />, onClick: () => openExternal(tab.id, e.path) },
          ]),
      { label: list.length > 1 ? tr("sftp.downloadN", { count: list.length }) : tr("sftp.download"), icon: <Download size={14} />, onClick: () => download(list) },
      { separator: true },
      { label: tr("sftp.cut"), icon: <Scissors size={14} />, shortcut: "Ctrl+X", onClick: () => toClip("move", list) },
      { label: tr("common.copy"), icon: <Copy size={14} />, shortcut: "Ctrl+C", onClick: () => toClip("copy", list) },
      {
        label: e.isDir ? tr("sftp.pasteHere") : tr("common.paste"),
        icon: <ClipboardPaste size={14} />,
        shortcut: "Ctrl+V",
        disabled: !clipFor,
        onClick: () => pasteClip(e.isDir ? e.path : dir),
      },
      { separator: true },
      { label: tr("common.rename"), icon: <Pencil size={14} />, shortcut: "F2", onClick: () => rename(e) },
      { label: tr("sftp.permissions"), icon: <KeyRound size={14} />, onClick: () => chmod(e) },
      { label: tr("sftp.copyPath"), icon: <Link2 size={14} />, shortcut: "Ctrl+Shift+C", onClick: () => Clipboard.SetText(list.map((x) => x.path).join("\n")) },
      { separator: true },
      { label: tr("common.delete"), icon: <Trash2 size={14} />, danger: true, onClick: () => remove(list) },
    ]);
  };

  const bgMenu = (ev: React.MouseEvent) =>
    openMenu(ev, [
      {
        label: clipFor ? tr(clipFor.mode === "move" ? "sftp.pasteCut" : "sftp.pasteCopy", { count: clipFor.paths.length }) : tr("common.paste"),
        icon: <ClipboardPaste size={14} />,
        shortcut: "Ctrl+V",
        disabled: !clipFor,
        onClick: () => pasteClip(dir),
      },
      { separator: true },
      {
        label: tr("sftp.newFolder"),
        icon: <FolderPlus size={14} />,
        onClick: async () => {
          const n = await inputDialog(tr("sftp.newFolder"), "");
          if (n) run(SFTPService.Mkdir(tab.id, join(dir, n)));
        },
      },
      {
        label: tr("sftp.newFile"),
        icon: <FilePlus size={14} />,
        onClick: async () => {
          const n = await inputDialog(tr("sftp.newFile"), "");
          if (n) {
            await run(SFTPService.NewFile(tab.id, join(dir, n)));
            openRemoteFile(tab.id, join(dir, n), true);
          }
        },
      },
      { label: tr("sftp.uploadFiles"), icon: <Upload size={14} />, onClick: () => SFTPService.UploadDialog(tab.id, dir).catch((e) => toast("error", errMsg(e))) },
      { label: tr("common.refresh"), icon: <RefreshCw size={14} />, onClick: () => load(dir) },
      { label: showHidden ? tr("sftp.hideHidden") : tr("sftp.showHidden"), onClick: () => setShowHidden(!showHidden) },
    ]);

  const allTransfers = useTransfers((x) => x.items);
  const dismissTransfer = useTransfers((x) => x.dismiss);
  const tlist = Object.values(allTransfers).filter((t) => t.tabId === tab.id);

  return (
    <div
      id={"sftp-drop-" + tab.id}
      data-file-drop-target
      className={"sftp-panel relative flex h-full flex-col bg-side " + (dropHover ? "outline-2 -outline-offset-2 outline-accent outline-dashed" : "")}
      onDragEnter={(e) => {
        if (isInternalDrag(e)) return;
        dropDepth.current++;
        setDropHover(true);
      }}
      onDragLeave={(e) => {
        if (isInternalDrag(e)) return;
        if (--dropDepth.current <= 0) {
          dropDepth.current = 0;
          setDropHover(false);
        }
      }}
    >
      <div className="flex items-center gap-0.5 border-b border-line px-1.5 py-1">
        <button className="icon-btn" title={tr("sftp.up")} onClick={goUp}>
          <ArrowUp size={15} />
        </button>
        <button className="icon-btn" title={tr("sftp.home")} onClick={() => SFTPService.Home(tab.id).then((h) => load(h))}>
          <Home size={15} />
        </button>
        <button className="icon-btn" title={tr("common.refresh")} onClick={() => load(dir)}>
          <RefreshCw size={15} className={loading ? "animate-spin" : ""} />
        </button>
        <button
          className="icon-btn"
          title={tr("sftp.newFolder")}
          onClick={async () => {
            const n = await inputDialog(tr("sftp.newFolder"), "");
            if (n) run(SFTPService.Mkdir(tab.id, join(dir, n)));
          }}
        >
          <FolderPlus size={15} />
        </button>
        <button className="icon-btn" title={tr("sftp.uploadHint")} onClick={() => SFTPService.UploadDialog(tab.id, dir).catch((e) => toast("error", errMsg(e)))}>
          <Upload size={15} />
        </button>
        <button className="icon-btn" title={tr("sftp.downloadSelected")} disabled={!selected.length} onClick={() => download(selected)}>
          <Download size={15} />
        </button>
        <button className="icon-btn" title={tr("sftp.deleteSelected")} disabled={!selected.length} onClick={() => remove(selected)}>
          <Trash2 size={15} />
        </button>
        <span
          className="icon-btn ml-auto cursor-help"
          title={[
            tr("sftp.keys.title"),
            tr("sftp.keys.move"),
            tr("sftp.keys.select"),
            tr("sftp.keys.open"),
            tr("sftp.keys.up"),
            tr("sftp.keys.renameDelete"),
            tr("sftp.keys.refreshNew"),
            tr("sftp.keys.transfer"),
            tr("sftp.keys.clip"),
            tr("sftp.keys.drag"),
            tr("sftp.keys.copyPath"),
            tr("sftp.keys.esc"),
          ].join("\n")}
        >
          <Keyboard size={15} />
        </span>
      </div>
      <div className="px-1.5 py-1">
        <input
          className="input font-mono text-[13px]"
          value={pathInput}
          onChange={(e) => setPathInput(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && load(pathInput.trim() || "/")}
        />
        <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-0.5 whitespace-nowrap px-0.5 text-[13px] text-mute">
          <Check
            checked={follow}
            title={tr("sftp.followHint")}
            onChange={(v) => {
              const st = useStore.getState().settings;
              if (!st) return;
              const ns = { ...st, sftpFollowTerminal: v };
              useStore.getState().setSettings(ns);
              SystemService.SaveSettings(ns).catch((e) => toast("error", errMsg(e)));
            }}
          >
            {tr("sftp.follow")}
          </Check>
          <Check checked={showHidden} onChange={setShowHidden}>
            {tr("sftp.showHidden")}
          </Check>
        </div>
      </div>
      {err && <div className="mx-2 mb-1 rounded border border-danger px-2 py-1 text-[13px] text-danger">{err}</div>}
      <div
        id="sftp-list"
        ref={listRef}
        tabIndex={0}
        className="group/list min-h-0 flex-1 overflow-auto outline-none"
        onKeyDown={onListKey}
        onContextMenu={(e) => e.target === e.currentTarget && bgMenu(e)}
      >
        <table className="w-full table-fixed text-[13px]">
          <thead className="sticky top-0 bg-side text-mute">
            <tr>
              <th className="whitespace-nowrap px-2 py-1 text-left font-normal">{tr("common.name")}</th>
              <th className="w-[60px] whitespace-nowrap px-1 text-right font-normal">{tr("common.size")}</th>
              <th className="sftp-col-time w-[92px] whitespace-nowrap px-1 text-left font-normal">{tr("common.modified")}</th>
              <th className="sftp-col-perm w-[84px] whitespace-nowrap px-1 text-left font-normal">{tr("sftp.colPerm")}</th>
            </tr>
          </thead>
          <tbody onContextMenu={(e) => e.target === e.currentTarget && bgMenu(e)}>
            {dir && dir !== "/" && (
              <tr
                data-key={UP}
                className={(dropDir === UP ? "bg-accent/30 " : "hover:bg-hover ") + (cursor === UP ? "group-focus/list:outline group-focus/list:outline-1 group-focus/list:-outline-offset-1 group-focus/list:outline-accent" : "")}
                onClick={() => {
                  setCursor(UP);
                  setSel(new Set());
                }}
                onDoubleClick={goUp}
                onDragOver={(ev) => {
                  if (!ev.dataTransfer.types.includes("pinion/sftp")) return;
                  ev.preventDefault();
                  ev.dataTransfer.dropEffect = ev.ctrlKey || ev.altKey ? "copy" : "move";
                  setDropDir(UP);
                }}
                onDragLeave={() => setDropDir("")}
                onDrop={(ev) => dropOn(ev, parent(dir))}
                title={tr("sftp.upRow")}
              >
                <td className="px-2 py-[3px]">
                  <span className="inline-flex items-center gap-1.5">
                    <Folder size={14} className="shrink-0 text-amber-500" />
                    <span>..</span>
                  </span>
                </td>
                <td />
                <td className="sftp-col-time" />
                <td className="sftp-col-perm" />
              </tr>
            )}
            {visible.map((e) => (
              <tr
                key={e.path}
                data-key={e.path}
                className={
                  (dropDir === e.path ? "bg-accent/30 " : "") +
                  (clipFor?.mode === "move" && clipFor.paths.includes(e.path) ? "opacity-50 " : "") +
                  (sel.has(e.path) ? "bg-sel" : "hover:bg-hover") +
                  (cursor === e.path ? " group-focus/list:outline group-focus/list:outline-1 group-focus/list:-outline-offset-1 group-focus/list:outline-accent" : "")
                }
                onClick={(ev) => {
                  setCursor(e.path);
                  const s = new Set(ev.metaKey || ev.ctrlKey ? sel : []);
                  if (ev.shiftKey && sel.size) {
                    const idx = visible.findIndex((x) => x.path === e.path);
                    const last = visible.findIndex((x) => sel.has(x.path));
                    const [a, b] = [Math.min(idx, last), Math.max(idx, last)];
                    visible.slice(a, b + 1).forEach((x) => s.add(x.path));
                  } else if (s.has(e.path) && (ev.metaKey || ev.ctrlKey)) s.delete(e.path);
                  else s.add(e.path);
                  setSel(s);
                }}
                onDoubleClick={() => openEntry(e)}
                draggable
                onDragEnd={() => setDropDir("")}
                onDragStart={(ev) => {
                  const list = sel.has(e.path) ? selected : [e];
                  ev.dataTransfer.setData("pinion/sftp", JSON.stringify({ tabId: tab.id, paths: list.map((x) => x.path) }));
                  ev.dataTransfer.effectAllowed = "copyMove";
                }}
                onDragOver={(ev) => {
                  if (!e.isDir || !ev.dataTransfer.types.includes("pinion/sftp")) return;
                  ev.preventDefault();
                  ev.dataTransfer.dropEffect = ev.ctrlKey || ev.altKey ? "copy" : "move";
                  setDropDir(e.path);
                }}
                onDragLeave={() => setDropDir("")}
                onDrop={(ev) => dropOn(ev, e.path)}
                onContextMenu={(ev) => {
                  setCursor(e.path);
                  rowMenu(ev, e);
                }}
                title={e.isLink ? `${e.path} → ${e.linkTarget}` : e.path}
              >
                <td className="truncate px-2 py-[3px]">
                  <span className="inline-flex items-center gap-1.5">
                    {e.isDir ? <Folder size={14} className="shrink-0 text-amber-500" /> : <File size={14} className="shrink-0 text-mute" />}
                    {e.isLink && <Link2 size={11} className="shrink-0 text-mute" />}
                    <span className="truncate">{e.name}</span>
                  </span>
                </td>
                <td className="whitespace-nowrap px-1 text-right text-mute">{e.isDir ? "" : fmtBytes(e.size)}</td>
                <td className="sftp-col-time truncate px-1 text-mute" title={fmtTime(e.mtime)}>{fmtShort(e.mtime)}</td>
                <td className="sftp-col-perm whitespace-nowrap px-1 font-mono text-[12px] text-mute">{e.mode}</td>
              </tr>
            ))}
          </tbody>
        </table>
        {!visible.length && !err && !loading && tab.state === "connected" && dir && <div className="p-4 text-center text-mute">{tr("sftp.emptyFolder")}</div>}
        {tab.state !== "connected" && !entries.length && <div className="p-4 text-center text-mute">{tr("sftp.waiting")}</div>}
      </div>
      {tlist.length > 0 && (
        <div className="max-h-40 overflow-auto border-t border-line px-2 py-1 text-[13px]">
          {tlist.map((t) => {
            const pct = t.totalBytes ? Math.round((t.bytes / t.totalBytes) * 100) : 0;
            return (
              <div key={t.id} className="py-1">
                <div className="flex items-center gap-1">
                  {t.kind === "upload" ? <Upload size={12} /> : t.kind === "download" ? <Download size={12} /> : t.kind === "move" ? <Scissors size={12} /> : <Copy size={12} />}
                  <span className="flex-1 truncate">{t.name}</span>
                  <span className="text-mute">
                    {t.filesDone}/{t.files} · {fmtBytes(t.bytes)}
                  </span>
                  <button
                    className="icon-btn !p-0"
                    title={t.state === "running" ? tr("sftp.cancelTransfer") : tr("common.remove")}
                    onClick={() => (t.state === "running" ? SFTPService.CancelTransfer(t.id) : dismissTransfer(t.id))}
                  >
                    <X size={12} />
                  </button>
                </div>
                {t.error && <div className="truncate text-[12px] text-danger" title={t.error}>{t.error}</div>}
                <div className="mt-0.5 h-1 rounded bg-line">
                  <div
                    className={"h-1 rounded " + (t.state === "error" ? "bg-danger" : t.state === "done" ? "bg-ok" : "bg-accent")}
                    style={{ width: `${t.state === "done" || t.state === "error" ? 100 : pct}%` }}
                  />
                </div>
              </div>
            );
          })}
        </div>
      )}
      {perm && (
        <PermDialog
          entry={perm}
          onClose={() => setPerm(null)}
          onApply={(mode) => {
            setPerm(null);
            run(SFTPService.Chmod(tab.id, perm.path, mode));
          }}
        />
      )}
    </div>
  );
}
