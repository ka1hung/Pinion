import { useState } from "react";
import { Send } from "lucide-react";
import { useStore } from "../lib/store";
import { TerminalService } from "../lib/api";
import { Check } from "./ui";

export default function MultiExecBar() {
  const { multi, setMulti, tabs } = useStore();
  const [cmd, setCmd] = useState("");
  const terms = tabs.filter((t) => t.kind === "terminal");
  const send = () => {
    if (!multi.selected.length) return;
    TerminalService.Broadcast(multi.selected, cmd + "\r");
    setCmd("");
  };
  return (
    <div className="flex items-center gap-2 border-t border-accent bg-side px-2 py-1.5">
      <span className="text-[13px] font-semibold text-accent">MultiExec</span>
      <button className="btn !py-0.5 text-[13px]" onClick={() => setMulti({ selected: terms.map((t) => t.id) })}>
        全選
      </button>
      <button className="btn !py-0.5 text-[13px]" onClick={() => setMulti({ selected: [] })}>
        全不選
      </button>
      <Check checked={multi.mirror} onChange={(v) => setMulti({ mirror: v })} title="在任一勾選分頁打字，同步送到其他勾選分頁">
        <span className="text-[13px]">同步鍵入</span>
      </Check>
      <input
        className="input flex-1 font-mono"
        placeholder={`輸入指令，Enter 送到 ${multi.selected.length} 個勾選的分頁`}
        value={cmd}
        onChange={(e) => setCmd(e.target.value)}
        onKeyDown={(e) => e.key === "Enter" && send()}
      />
      <button className="btn-primary" disabled={!multi.selected.length} onClick={send}>
        <Send size={14} /> 送出
      </button>
    </div>
  );
}
