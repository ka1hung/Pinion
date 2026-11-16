import { useState } from "react";
import { useStore } from "../../lib/store";
import { openAdhoc, openSession, parseQuick } from "../../lib/actions";
import { Modal } from "../ui";
import { typeIcon } from "../Sidebar";

export default function QuickConnect() {
  const { sessions, set } = useStore();
  const [q, setQ] = useState("");
  const [idx, setIdx] = useState(0);
  const t = q.trim().toLowerCase();
  const matches = sessions
    .filter((s) => !t || [s.name, s.folder, s.ssh?.host ?? "", s.telnet?.host ?? "", s.serial?.port ?? ""].some((x) => x.toLowerCase().includes(t)))
    .slice(0, 12);
  const close = () => set("quickOpen", false);
  const go = (i = idx) => {
    if (matches[i]) openSession(matches[i].id);
    else {
      const s = parseQuick(q);
      if (s) openAdhoc(s);
    }
    close();
  };
  return (
    <Modal title="快速連線" onClose={close} width={520}>
      <input
        autoFocus
        className="input"
        placeholder="搜尋 session，或輸入 user@host:port / telnet://host / COM3"
        value={q}
        onChange={(e) => {
          setQ(e.target.value);
          setIdx(0);
        }}
        onKeyDown={(e) => {
          if (e.key === "ArrowDown") setIdx((i) => Math.min(i + 1, matches.length - 1));
          if (e.key === "ArrowUp") setIdx((i) => Math.max(i - 1, 0));
          if (e.key === "Enter") go();
        }}
      />
      <div className="mt-2">
        {matches.map((s, i) => (
          <div key={s.id} className={"flex cursor-default items-center gap-2 rounded px-2 py-1 " + (i === idx ? "bg-sel" : "hover:bg-hover")} onMouseEnter={() => setIdx(i)} onClick={() => go(i)}>
            {typeIcon(s.type)} <span>{s.name}</span>
            <span className="ml-auto text-[13px] text-mute">{s.folder}</span>
          </div>
        ))}
        {!matches.length && q && <div className="px-2 py-1 text-mute">Enter 直接連線到「{q}」</div>}
      </div>
    </Modal>
  );
}
