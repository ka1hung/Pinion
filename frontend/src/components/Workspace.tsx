import { X, FileText, RotateCw, Columns2, Copy as CopyIcon } from "lucide-react";
import { useStore, type Tab } from "../lib/store";
import { closeTab, openSession } from "../lib/actions";
import { TerminalService } from "../lib/api";
import { typeIcon } from "./Sidebar";
import { openMenu } from "./ui";
import TerminalView from "./TerminalView";
import EditorView from "./EditorView";
import Welcome from "./Welcome";

function stateColor(state: string) {
  switch (state) {
    case "connected":
      return "bg-ok";
    case "connecting":
    case "reconnecting":
      return "bg-warn animate-pulse";
    default:
      return "bg-danger";
  }
}

function TabBar() {
  const { tabs, activeId, split, multi, activate, toggleSelected, setSplit } = useStore();
  const menu = (e: React.MouseEvent, t: Tab) =>
    openMenu(e, [
      { label: "關閉", icon: <X size={14} />, onClick: () => closeTab(t.id) },
      {
        label: "關閉其他分頁",
        onClick: () => tabs.filter((x) => x.id !== t.id).forEach((x) => closeTab(x.id)),
      },
      { separator: true },
      ...(t.kind === "terminal"
        ? [
            { label: "重新連線", icon: <RotateCw size={14} />, onClick: () => TerminalService.Reconnect(t.id) },
            {
              label: "再開一個相同連線",
              icon: <CopyIcon size={14} />,
              disabled: !t.sessionId,
              onClick: () => openSession(t.sessionId),
            },
          ]
        : []),
      {
        label: "顯示在右側窗格",
        icon: <Columns2 size={14} />,
        onClick: () => setSplit({ on: true, rightId: t.id }),
      },
    ]);
  return (
    <div className="flex h-9 shrink-0 items-end overflow-x-auto border-b border-line bg-side">
      {tabs.map((t, i) => {
        const active = t.id === activeId || (split.on && t.id === split.rightId);
        return (
          <div
            key={t.id}
            className={
              "group flex h-full max-w-[220px] shrink-0 cursor-default items-center gap-1.5 border-r border-line px-2.5 " +
              (t.id === activeId
                ? "bg-base text-fg shadow-[inset_0_2px_0_var(--c-accent)]"
                : active
                  ? "bg-base text-fg shadow-[inset_0_2px_0_var(--c-mute)]"
                  : "text-mute hover:bg-hover")
            }
            onMouseDown={(e) => {
              if (e.button === 1) {
                e.preventDefault();
                closeTab(t.id);
              }
            }}
            onClick={() => activate(t.id)}
            onContextMenu={(e) => menu(e, t)}
            title={t.kind === "terminal" ? `${t.describe || t.title}${t.message ? "\n" + t.message : ""}` : t.path}
          >
            {multi.on && t.kind === "terminal" && (
              <input
                type="checkbox"
                className="chk"
                checked={multi.selected.includes(t.id)}
                onClick={(e) => e.stopPropagation()}
                onChange={() => toggleSelected(t.id)}
              />
            )}
            {t.kind === "terminal" ? (
              <>
                <span className={"h-2 w-2 shrink-0 rounded-full " + stateColor(t.state)} />
                {typeIcon(t.type, 13)}
              </>
            ) : (
              <FileText size={13} className="shrink-0 text-sky-400" />
            )}
            <span className="truncate">
              {i < 9 && <span className="mr-1 text-[11px] opacity-50">{i + 1}</span>}
              {t.title}
            </span>
            {t.kind === "editor" && t.dirty ? (
              <span className="ml-1 h-2 w-2 shrink-0 rounded-full bg-fg group-hover:hidden" />
            ) : null}
            <button
              className={
                "icon-btn ml-0.5 !p-0.5 " + (t.kind === "editor" && t.dirty ? "hidden group-hover:inline-flex" : "opacity-0 group-hover:opacity-100")
              }
              onClick={(e) => {
                e.stopPropagation();
                closeTab(t.id);
              }}
            >
              <X size={13} />
            </button>
          </div>
        );
      })}
    </div>
  );
}

function Pane({ tab, side }: { tab: Tab | undefined; side: "left" | "right" }) {
  const split = useStore((s) => s.split);
  const setSplit = useStore((s) => s.setSplit);
  const focused = split.on && split.focus === side;
  return (
    <div
      className={"relative flex min-w-0 flex-1 flex-col " + (split.on ? "border-t-2 " + (focused ? "border-accent" : "border-transparent") : "")}
      onMouseDownCapture={() => split.on && split.focus !== side && setSplit({ focus: side })}
    >
      {tab ? (
        tab.kind === "terminal" ? (
          <TerminalView key={tab.id} tab={tab} />
        ) : (
          <EditorView key={tab.id} tab={tab} />
        )
      ) : side === "right" ? (
        <div className="flex flex-1 items-center justify-center text-mute">
          點這個窗格後再點上方分頁，或在分頁右鍵選「顯示在右側窗格」
        </div>
      ) : (
        <Welcome />
      )}
    </div>
  );
}

export default function Workspace() {
  const { tabs, activeId, split } = useStore();
  const left = tabs.find((t) => t.id === activeId);
  const right = split.on && split.rightId !== activeId ? tabs.find((t) => t.id === split.rightId) : undefined;
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {tabs.length > 0 && <TabBar />}
      <div className="flex min-h-0 flex-1">
        <Pane tab={left} side="left" />
        {split.on && (
          <>
            <div className="w-px bg-line" />
            <Pane tab={right} side="right" />
          </>
        )}
      </div>
    </div>
  );
}
