import { useEffect, useMemo, useState } from "react";
import {
  ChevronRight, ChevronDown, Folder, FolderOpen, Server, Cpu, Network, TerminalSquare, Search,
  Plus, FolderPlus, Pencil, Copy, Trash2, Play, Upload, Download, Cable, HardDrive, FolderTree,
} from "lucide-react";
import { useStore, toast, type SidebarView } from "../lib/store";
import { SessionService, errMsg, type store as S } from "../lib/api";
import { openSession } from "../lib/actions";
import { openMenu, confirmDialog, inputDialog } from "./ui";
import TunnelsPanel from "./panels/TunnelsPanel";
import ServersPanel from "./panels/ServersPanel";
import SftpPanel from "./SftpPanel";

export function typeIcon(type: string, size = 14) {
  switch (type) {
    case "ssh":
      return <Server size={size} className="text-sky-500" />;
    case "serial":
      return <Cpu size={size} className="text-amber-500" />;
    case "telnet":
      return <TerminalSquare size={size} className="text-violet-500" />;
    case "tcp":
    case "udp":
      return <Network size={size} className="text-emerald-500" />;
    default:
      return <TerminalSquare size={size} className="text-mute" />;
  }
}

function sessionSubtitle(s: S.Session): string {
  if (s.ssh) return `${s.ssh.user ? s.ssh.user + "@" : ""}${s.ssh.host}${s.ssh.port !== 22 ? ":" + s.ssh.port : ""}`;
  if (s.telnet) return `${s.telnet.host}:${s.telnet.port}`;
  if (s.serial) return `${s.serial.port} ${s.serial.baud}`;
  if (s.net) return `${s.type.toUpperCase()} ${s.net.mode} ${s.net.host}:${s.net.port}`;
  return s.type;
}

interface Node {
  name: string;
  path: string;
  children: Map<string, Node>;
  sessions: S.Session[];
}

function buildTree(folders: string[], sessions: S.Session[]): Node {
  const root: Node = { name: "", path: "", children: new Map(), sessions: [] };
  const ensure = (path: string) => {
    let n = root;
    if (!path) return n;
    let acc = "";
    for (const part of path.split("/")) {
      acc = acc ? acc + "/" + part : part;
      if (!n.children.has(part)) n.children.set(part, { name: part, path: acc, children: new Map(), sessions: [] });
      n = n.children.get(part)!;
    }
    return n;
  };
  folders.forEach(ensure);
  sessions.forEach((s) => ensure(s.folder).sessions.push(s));
  return root;
}

/** The terminal tab shown in the focused pane (split-aware). */
export function useFocusedTab() {
  return useStore((s) => {
    const id = s.split.on && s.split.focus === "right" && s.split.rightId ? s.split.rightId : s.activeId;
    return s.tabs.find((t) => t.id === id);
  });
}

export default function Sidebar() {
  const view = useStore((s) => s.sidebar);
  const set = useStore((s) => s.set);
  const autoSwitch = useStore((s) => s.settings?.sftpAutoSwitch ?? true);
  const [width, setWidth] = useState(() => Number(localStorage.getItem("pinion.sidebarWidth")) || 300);
  const focused = useFocusedTab();
  // SFTP target: the focused SSH tab, or the SSH tab a remote editor came from
  const sshTab = useStore((s) => {
    if (focused?.kind === "terminal" && focused.type === "ssh") return focused;
    if (focused?.kind === "editor" && focused.termTabId) {
      const t = s.tabs.find((x) => x.id === focused.termTabId);
      if (t?.kind === "terminal") return t;
    }
    return undefined;
  });

  // One simple rule: switching to an SSH tab shows its SFTP files,
  // switching to anything else shows the session list. A manual choice
  // (tunnels, servers...) holds until the next tab switch.
  useEffect(() => {
    if (!autoSwitch) return;
    set("sidebar", sshTab ? "sftp" : "sessions");
  }, [focused?.id, autoSwitch]);

  const tabs: { id: SidebarView; label: string; icon: JSX.Element }[] = [
    { id: "sessions", label: "Sessions", icon: <Server size={14} /> },
    { id: "sftp", label: "SFTP", icon: <FolderTree size={14} /> },
    { id: "tunnels", label: "隧道", icon: <Cable size={14} /> },
    { id: "servers", label: "伺服器", icon: <HardDrive size={14} /> },
  ];
  const shown = view;
  const choose = (v: SidebarView) => set("sidebar", v);
  const startDrag = (e: React.MouseEvent) => {
    const x0 = e.clientX;
    const w0 = width;
    let w = w0;
    const move = (ev: MouseEvent) => {
      w = Math.max(220, Math.min(700, w0 + ev.clientX - x0));
      setWidth(w);
    };
    const up = () => {
      localStorage.setItem("pinion.sidebarWidth", String(w));
      window.removeEventListener("mousemove", move);
      window.removeEventListener("mouseup", up);
    };
    window.addEventListener("mousemove", move);
    window.addEventListener("mouseup", up);
  };
  return (
    <div className="relative flex shrink-0 flex-col border-r border-line bg-side" style={{ width }}>
      <div className="flex border-b border-line">
        {tabs.map((t) => (
          <button
            key={t.id}
            className={
              "flex flex-1 items-center justify-center gap-1 py-1.5 text-[13px] " +
              (shown === t.id ? "border-b-2 border-accent text-fg" : "text-mute hover:text-fg")
            }
            onClick={() => choose(t.id)}
          >
            {t.icon}
            {t.label}
          </button>
        ))}
      </div>
      <div className="min-h-0 flex-1">
        {shown === "sessions" && <SessionTree />}
        {shown === "sftp" &&
          (sshTab ? (
            <SftpPanel key={sshTab.id} tab={sshTab} />
          ) : (
            <div className="p-4 text-center text-mute">
              切到一個 SSH 分頁就會在這裡顯示它的遠端檔案。
              <br />
              <button className="mt-2 text-accent" onClick={() => set("sidebar", "sessions")}>
                回到 Sessions 列表
              </button>
            </div>
          ))}
        {shown === "tunnels" && <TunnelsPanel />}
        {shown === "servers" && <ServersPanel />}
      </div>
      <div className="absolute right-[-3px] top-0 z-10 h-full w-[6px] cursor-col-resize" onMouseDown={startDrag} />
    </div>
  );
}

function SessionTree() {
  const { folders, sessions, set } = useStore();
  const [q, setQ] = useState("");
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({});
  const [selected, setSelected] = useState<string | null>(null);

  const filtered = useMemo(() => {
    const t = q.trim().toLowerCase();
    if (!t) return sessions;
    return sessions.filter((s) =>
      [s.name, s.folder, sessionSubtitle(s), s.notes ?? ""].some((x) => x.toLowerCase().includes(t)),
    );
  }, [q, sessions]);
  const tree = useMemo(() => buildTree(q ? [] : folders, filtered), [folders, filtered, q]);

  const newFolder = async (parent: string) => {
    const name = await inputDialog("新增資料夾", "", parent ? `建立於「${parent}」底下` : "例如案場名稱");
    if (!name) return;
    SessionService.AddFolder(parent ? parent + "/" + name : name).catch((e) => toast("error", errMsg(e)));
  };

  const folderMenu = (e: React.MouseEvent, n: Node) =>
    openMenu(e, [
      { label: "在此新增 Session", icon: <Plus size={14} />, onClick: () => set("sessionEditor", { folder: n.path }) },
      { label: "新增子資料夾", icon: <FolderPlus size={14} />, onClick: () => newFolder(n.path) },
      {
        label: "全部連線",
        icon: <Play size={14} />,
        onClick: () => collect(n).forEach((s) => openSession(s.id)),
      },
      { separator: true },
      {
        label: "重新命名",
        icon: <Pencil size={14} />,
        onClick: async () => {
          const v = await inputDialog("重新命名資料夾", n.path, "可用 / 移到其他位置");
          if (v && v !== n.path) SessionService.RenameFolder(n.path, v).catch((er) => toast("error", errMsg(er)));
        },
      },
      {
        label: "匯出此資料夾",
        icon: <Upload size={14} />,
        onClick: () =>
          SessionService.Export(n.path)
            .then((p) => p && toast("success", "已匯出到 " + p))
            .catch((er) => toast("error", errMsg(er))),
      },
      {
        label: "匯入到此資料夾",
        icon: <Download size={14} />,
        onClick: () =>
          SessionService.ImportPinion(n.path)
            .then((c) => c && toast("success", `已匯入 ${c} 個 session`))
            .catch((er) => toast("error", errMsg(er))),
      },
      { separator: true },
      {
        label: "刪除資料夾",
        icon: <Trash2 size={14} />,
        danger: true,
        onClick: async () => {
          const count = collect(n).length;
          if (
            await confirmDialog(
              "刪除資料夾",
              count ? `「${n.path}」內有 ${count} 個 session，會一併刪除（含已存密碼）。` : `刪除「${n.path}」？`,
              { danger: true, ok: "刪除" },
            )
          )
            SessionService.DeleteFolder(n.path, true).catch((er) => toast("error", errMsg(er)));
        },
      },
    ]);

  const sessionMenu = (e: React.MouseEvent, s: S.Session) => {
    setSelected(s.id);
    openMenu(e, [
      { label: "連線", icon: <Play size={14} />, onClick: () => openSession(s.id) },
      { label: "編輯", icon: <Pencil size={14} />, onClick: () => set("sessionEditor", { session: s }) },
      {
        label: "複製",
        icon: <Copy size={14} />,
        onClick: () => SessionService.Duplicate(s.id).catch((er) => toast("error", errMsg(er))),
      },
      { separator: true },
      {
        label: "刪除",
        icon: <Trash2 size={14} />,
        danger: true,
        onClick: async () => {
          if (await confirmDialog("刪除 Session", `刪除「${s.name}」及其已存密碼？`, { danger: true, ok: "刪除" }))
            SessionService.Delete(s.id).catch((er) => toast("error", errMsg(er)));
        },
      },
    ]);
  };

  const onDrop = (e: React.DragEvent, folder: string) => {
    const id = e.dataTransfer.getData("pinion/session");
    if (!id) return;
    e.preventDefault();
    e.stopPropagation();
    SessionService.Move(id, folder).catch((er) => toast("error", errMsg(er)));
  };

  const renderNode = (n: Node, depth: number): JSX.Element[] => {
    const out: JSX.Element[] = [];
    const kids = [...n.children.values()].sort((a, b) => a.name.localeCompare(b.name));
    for (const c of kids) {
      const isCol = !q && collapsed[c.path];
      out.push(
        <div
          key={"f:" + c.path}
          className="flex cursor-default items-center gap-1 py-[3px] pr-2 hover:bg-hover"
          style={{ paddingLeft: 6 + depth * 14 }}
          onClick={() => setCollapsed((x) => ({ ...x, [c.path]: !x[c.path] }))}
          onContextMenu={(e) => folderMenu(e, c)}
          onDragOver={(e) => e.preventDefault()}
          onDrop={(e) => onDrop(e, c.path)}
        >
          {isCol ? <ChevronRight size={14} className="text-mute" /> : <ChevronDown size={14} className="text-mute" />}
          {isCol ? <Folder size={14} className="text-amber-500" /> : <FolderOpen size={14} className="text-amber-500" />}
          <span className="truncate">{c.name}</span>
          <span className="ml-auto text-[12px] text-mute">{collect(c).length || ""}</span>
        </div>,
      );
      if (!isCol) out.push(...renderNode(c, depth + 1));
    }
    for (const s of n.sessions) {
      out.push(
        <div
          key={"s:" + s.id}
          draggable
          onDragStart={(e) => e.dataTransfer.setData("pinion/session", s.id)}
          className={
            "group flex cursor-default items-center gap-1.5 py-[3px] pr-2 " +
            (selected === s.id ? "bg-sel" : "hover:bg-hover")
          }
          style={{ paddingLeft: 6 + depth * 14 + 16 }}
          onClick={() => setSelected(s.id)}
          onDoubleClick={() => openSession(s.id)}
          onContextMenu={(e) => sessionMenu(e, s)}
          title={`${sessionSubtitle(s)}${s.notes ? "\n" + s.notes : ""}`}
        >
          {s.color && <span className="h-2 w-2 shrink-0 rounded-full" style={{ background: s.color }} />}
          {typeIcon(s.type)}
          <span className="truncate">{s.name}</span>
          <span className="ml-auto truncate pl-2 text-[12px] text-mute opacity-0 group-hover:opacity-100">
            {sessionSubtitle(s)}
          </span>
        </div>,
      );
    }
    return out;
  };

  return (
    <div className="flex h-full flex-col">
      <div className="flex items-center gap-1 p-2">
        <div className="relative flex-1">
          <Search size={13} className="absolute left-2 top-1.5 text-mute" />
          <input className="input pl-7" placeholder="搜尋名稱、IP、案場…" value={q} onChange={(e) => setQ(e.target.value)} />
        </div>
        <button className="icon-btn" title="新增資料夾" onClick={() => newFolder("")}>
          <FolderPlus size={16} />
        </button>
        <button className="icon-btn" title="新增 Session" onClick={() => set("sessionEditor", {})}>
          <Plus size={16} />
        </button>
      </div>
      <div
        className="min-h-0 flex-1 overflow-auto pb-4"
        onDragOver={(e) => e.preventDefault()}
        onDrop={(e) => onDrop(e, "")}
        onContextMenu={(e) =>
          e.target === e.currentTarget &&
          openMenu(e, [
            { label: "新增 Session", icon: <Plus size={14} />, onClick: () => set("sessionEditor", {}) },
            { label: "新增資料夾", icon: <FolderPlus size={14} />, onClick: () => newFolder("") },
            {
              label: "匯出全部",
              icon: <Upload size={14} />,
              onClick: () => SessionService.Export("").then((p) => p && toast("success", "已匯出到 " + p)),
            },
            {
              label: "匯入 Pinion JSON",
              icon: <Download size={14} />,
              onClick: () => SessionService.ImportPinion("").then((c) => c && toast("success", `已匯入 ${c} 個 session`)),
            },
          ])
        }
      >
        {renderNode(tree, 0)}
        {sessions.length === 0 && (
          <div className="p-4 text-center text-mute">
            還沒有 session。
            <br />
            <button className="mt-2 text-accent" onClick={() => set("sessionEditor", {})}>
              新增一個
            </button>
            {" 或 "}
            <button className="text-accent" onClick={() => set("importOpen", true)}>
              匯入 Session 設定
            </button>
          </div>
        )}
      </div>
    </div>
  );
}

function collect(n: Node): S.Session[] {
  return [...n.sessions, ...[...n.children.values()].flatMap(collect)];
}
