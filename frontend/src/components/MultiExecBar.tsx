import { useState } from "react";
import { Send } from "lucide-react";
import { useStore } from "../lib/store";
import { TerminalService } from "../lib/api";
import { Check } from "./ui";
import { useT } from "../lib/i18n";

export default function MultiExecBar() {
  const t = useT();
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
        {t("multiExec.selectAll")}
      </button>
      <button className="btn !py-0.5 text-[13px]" onClick={() => setMulti({ selected: [] })}>
        {t("multiExec.selectNone")}
      </button>
      <Check checked={multi.mirror} onChange={(v) => setMulti({ mirror: v })} title={t("multiExec.mirrorTitle")}>
        <span className="text-[13px]">{t("multiExec.mirror")}</span>
      </Check>
      <input
        className="input flex-1 font-mono"
        placeholder={t("multiExec.placeholder", { count: multi.selected.length })}
        value={cmd}
        onChange={(e) => setCmd(e.target.value)}
        onKeyDown={(e) => e.key === "Enter" && send()}
      />
      <button className="btn-primary" disabled={!multi.selected.length} onClick={send}>
        <Send size={14} /> {t("multiExec.send")}
      </button>
    </div>
  );
}
