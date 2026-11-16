import { Plus, Terminal, Download, Zap } from "lucide-react";
import { useStore } from "../lib/store";
import { openLocal, openSession } from "../lib/actions";
import { isMac } from "../lib/api";
import { typeIcon } from "./Sidebar";

export default function Welcome() {
  const { sessions, set } = useStore();
  const recent = [...sessions].sort((a, b) => (b.updatedAt > a.updatedAt ? 1 : -1)).slice(0, 8);
  const mod = isMac ? "⌘" : "Ctrl+Shift+";
  return (
    <div className="flex flex-1 items-center justify-center overflow-auto">
      <div className="w-[520px] max-w-full p-6">
        <div className="mb-1 flex items-center gap-3">
          <img src="/icon.png" className="h-12 w-12" alt="" />
          <div>
            <div className="text-2xl font-semibold">Pinion</div>
            <div className="text-mute">SSH · Serial · Telnet · SFTP · 隧道 · 臨時伺服器</div>
          </div>
        </div>
        <div className="mt-6 grid grid-cols-2 gap-2">
          <button className="btn justify-start !py-2" onClick={() => set("sessionEditor", {})}>
            <Plus size={16} /> 新增 Session <span className="ml-auto text-mute">{mod}N</span>
          </button>
          <button className="btn justify-start !py-2" onClick={openLocal}>
            <Terminal size={16} /> 本機終端 <span className="ml-auto text-mute">{mod}T</span>
          </button>
          <button className="btn justify-start !py-2" onClick={() => set("quickOpen", true)}>
            <Zap size={16} /> 快速連線 <span className="ml-auto text-mute">{mod}K</span>
          </button>
          <button className="btn justify-start !py-2" onClick={() => set("importOpen", true)}>
            <Download size={16} /> 匯入 Session 設定
          </button>
        </div>
        {recent.length > 0 && (
          <>
            <div className="mb-1 mt-6 text-mute">最近的 Session</div>
            <div className="rounded border border-line">
              {recent.map((s) => (
                <button
                  key={s.id}
                  className="flex w-full items-center gap-2 border-b border-line px-3 py-1.5 text-left last:border-0 hover:bg-hover"
                  onClick={() => openSession(s.id)}
                >
                  {typeIcon(s.type)}
                  <span className="truncate">{s.name}</span>
                  <span className="ml-auto truncate text-[13px] text-mute">{s.folder}</span>
                </button>
              ))}
            </div>
          </>
        )}
      </div>
    </div>
  );
}
