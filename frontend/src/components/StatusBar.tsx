import { useStore } from "../lib/store";

export default function StatusBar() {
  const { tabs, activeId, multi } = useStore();
  const t = tabs.find((x) => x.id === activeId);
  const connected = tabs.filter((x) => x.kind === "terminal" && x.state === "connected").length;
  return (
    <div className="flex h-6 shrink-0 items-center gap-3 border-t border-line bg-side px-3 text-[13px] text-mute">
      {t?.kind === "terminal" && (
        <span>
          {t.describe || t.title}
          {t.state !== "connected" && ` — ${t.state}${t.message ? ": " + t.message : ""}`}
        </span>
      )}
      {t?.kind === "editor" && <span className="font-mono">{t.path}</span>}
      <div className="flex-1" />
      {multi.on && <span className="text-accent">MultiExec：{multi.selected.length} 個分頁</span>}
      <span>{connected} 個連線</span>
    </div>
  );
}
