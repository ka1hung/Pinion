import { useEffect, useRef, useState } from "react";
import {
  FolderTree, FileDown, Eraser, Search, RotateCw, Binary, Clock, Braces, ScrollText, ChevronUp, ChevronDown, X, Send,
} from "lucide-react";
import { useStore, toast, type TermTab } from "../lib/store";
import { terminals } from "../lib/terminals";
import { TerminalService, SystemService, errMsg } from "../lib/api";
import { Check } from "./ui";
import { useT } from "../lib/i18n";

function Lights({ id }: { id: string }) {
  const tr = useT();
  const [, force] = useState(0);
  useEffect(() => {
    const inst = terminals.get(id);
    if (!inst) return;
    let t: number | null = null;
    const l = () => {
      force((x) => x + 1);
      if (t) clearTimeout(t);
      t = window.setTimeout(() => force((x) => x + 1), 120);
    };
    inst.listeners.add(l);
    return () => {
      inst.listeners.delete(l);
      if (t) clearTimeout(t);
    };
  }, [id]);
  const a = terminals.get(id)?.activity ?? { tx: 0, rx: 0 };
  const now = performance.now();
  const dot = (on: boolean, color: string) => (
    <span className={"inline-block h-2.5 w-2.5 rounded-full border border-line " + (on ? color : "bg-transparent")} />
  );
  return (
    <span className="flex items-center gap-1.5 text-[12px] text-mute" title={tr("terminal.activity")}>
      {dot(now - a.tx < 120, "bg-ok")} TX {dot(now - a.rx < 120, "bg-danger")} RX
    </span>
  );
}

function ToolBtn(p: { on?: boolean; title: string; onClick: () => void; children: React.ReactNode }) {
  return (
    <button
      className={"icon-btn flex items-center gap-1 !px-1.5 text-[13px] " + (p.on ? "!bg-sel !text-fg" : "")}
      title={p.title}
      onClick={p.onClick}
    >
      {p.children}
    </button>
  );
}

function SendBar({ tab }: { tab: TermTab }) {
  const t = useT();
  const sess = useStore((s) => s.sessions.find((x) => x.id === tab.sessionId));
  const isNet = tab.type === "tcp" || tab.type === "udp";
  const [text, setText] = useState("");
  const [format, setFormat] = useState<string>(sess?.net?.format || "text");
  const [ending, setEnding] = useState<string>(isNet ? sess?.net?.appendLine || "none" : sess?.serial?.sendLineEnd || "cr");
  const [hist, setHist] = useState<string[]>([]);
  const [hi, setHi] = useState(-1);
  const send = async () => {
    if (!text && format !== "text") return;
    try {
      await TerminalService.Send(tab.id, text, format, ending);
      terminals.get(tab.id)?.listeners.forEach((l) => l());
      setHist((h) => [text, ...h.filter((x) => x !== text)].slice(0, 50));
      setHi(-1);
      setText("");
    } catch (e) {
      toast("error", errMsg(e));
    }
  };
  return (
    <div className="flex items-center gap-1.5 border-t border-line bg-side px-2 py-1.5">
      <select className="input w-24" value={format} onChange={(e) => setFormat(e.target.value)}>
        <option value="text">UTF-8</option>
        <option value="hex">HEX</option>
        <option value="json">JSON</option>
      </select>
      <input
        className="input flex-1 font-mono"
        placeholder={format === "hex" ? "01 03 00 00 00 0A C5 CD" : format === "json" ? '{"cmd":"status"}' : t("terminal.sendPlaceholder")}
        value={text}
        onChange={(e) => setText(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter" && !e.shiftKey) {
            e.preventDefault();
            send();
          } else if (e.key === "ArrowUp" && hist.length) {
            const n = Math.min(hi + 1, hist.length - 1);
            setHi(n);
            setText(hist[n]);
          } else if (e.key === "ArrowDown") {
            const n = hi - 1;
            setHi(n);
            setText(n >= 0 ? hist[n] : "");
          }
        }}
      />
      <select className="input w-24" value={ending} onChange={(e) => setEnding(e.target.value)} title={t("terminal.lineEnding")}>
        <option value="none">{t("terminal.noEnding")}</option>
        <option value="cr">CR</option>
        <option value="lf">LF</option>
        <option value="crlf">CRLF</option>
      </select>
      <button className="btn-primary" onClick={send}>
        <Send size={14} /> {t("terminal.send")}
      </button>
    </div>
  );
}

function SearchBar({ id, onClose }: { id: string; onClose: () => void }) {
  const t = useT();
  const [q, setQ] = useState("");
  const inst = terminals.get(id);
  const opts = { caseSensitive: false, decorations: { matchOverviewRuler: "#888", activeMatchColorOverviewRuler: "#fa0" } } as any;
  return (
    <div className="absolute right-4 top-2 z-10 flex items-center gap-1 rounded border border-line bg-base p-1 shadow">
      <input
        autoFocus
        className="input w-56"
        placeholder={t("terminal.search")}
        value={q}
        onChange={(e) => {
          setQ(e.target.value);
          inst?.search.findNext(e.target.value, { ...opts, incremental: true });
        }}
        onKeyDown={(e) => {
          if (e.key === "Enter") (e.shiftKey ? inst?.search.findPrevious(q, opts) : inst?.search.findNext(q, opts));
          if (e.key === "Escape") onClose();
        }}
      />
      <button className="icon-btn" onClick={() => inst?.search.findPrevious(q, opts)}>
        <ChevronUp size={14} />
      </button>
      <button className="icon-btn" onClick={() => inst?.search.findNext(q, opts)}>
        <ChevronDown size={14} />
      </button>
      <button
        className="icon-btn"
        onClick={() => {
          inst?.search.clearDecorations();
          onClose();
          inst?.focus();
        }}
      >
        <X size={14} />
      </button>
    </div>
  );
}

export default function TerminalView({ tab }: { tab: TermTab }) {
  const t = useT();
  const ref = useRef<HTMLDivElement>(null);
  const updateTab = useStore((s) => s.updateTab);
  const [searching, setSearching] = useState(false);
  const sftpShown = useStore((s) => s.sidebarOpen && s.sidebar === "sftp");
  const [dtr, setDtr] = useState(true);
  const [rts, setRts] = useState(true);

  useEffect(() => {
    const inst = terminals.ensure(tab.id);
    if (ref.current) inst.attach(ref.current);
    inst.focus();
    const onSearch = (e: Event) => (e as CustomEvent).detail === tab.id && setSearching(true);
    window.addEventListener("pinion:term-search", onSearch);
    return () => window.removeEventListener("pinion:term-search", onSearch);
  }, [tab.id]);

  const setView = (patch: Partial<TermTab["view"]>) => {
    const view = { ...tab.view, ...patch };
    terminals.get(tab.id)?.setView(view);
    updateTab(tab.id, { view });
  };

  const toggleLog = async () => {
    try {
      const p = await TerminalService.SetLogging(tab.id, !tab.logPath);
      if (p) toast("info", t("terminal.logStarted", { path: p }));
    } catch (e) {
      toast("error", errMsg(e));
    }
  };

  const saveOutput = async () => {
    const inst = terminals.get(tab.id);
    if (!inst) return;
    const buf = inst.term.buffer.active;
    const lines: string[] = [];
    for (let i = 0; i < buf.length; i++) lines.push(buf.getLine(i)?.translateToString(true) ?? "");
    try {
      const p = await TerminalService.SaveOutput(`${tab.title}.log`, lines.join("\n").trimEnd() + "\n");
      if (p) toast("success", t("terminal.saved", { path: p }));
    } catch (e) {
      toast("error", errMsg(e));
    }
  };

  const isSerial = tab.type === "serial";
  const isNet = tab.type === "tcp" || tab.type === "udp";
  const lc = (op: string, on: boolean) => TerminalService.LineControl(tab.id, op, on).catch((e) => toast("error", errMsg(e)));

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex h-8 shrink-0 items-center gap-1 border-b border-line bg-base px-2">
        <span className="truncate text-[13px] text-mute">{tab.describe || tab.title}</span>
        {tab.state === "disconnected" && (
          <button className="btn ml-2 !py-0.5 text-[13px]" onClick={() => TerminalService.Reconnect(tab.id)}>
            <RotateCw size={13} /> {t("terminal.reconnect")}
          </button>
        )}
        <div className="flex-1" />
        {(isSerial || isNet) && (
          <>
            <Lights id={tab.id} />
            <div className="mx-1 h-4 border-l border-line" />
            <ToolBtn on={tab.view.hex} title={t("terminal.hexTitle")} onClick={() => setView({ hex: !tab.view.hex })}>
              <Binary size={14} /> HEX
            </ToolBtn>
            {isNet && (
              <ToolBtn on={tab.view.json} title={t("terminal.jsonTitle")} onClick={() => setView({ json: !tab.view.json })}>
                <Braces size={14} /> JSON
              </ToolBtn>
            )}
            <ToolBtn on={tab.view.timestamp} title={t("terminal.timestampTitle")} onClick={() => setView({ timestamp: !tab.view.timestamp })}>
              <Clock size={14} /> {t("terminal.time")}
            </ToolBtn>
          </>
        )}
        {isSerial && (
          <>
            <div className="mx-1 h-4 border-l border-line" />
            <Check checked={dtr} onChange={(v) => (setDtr(v), lc("dtr", v))}>
              <span className="text-[13px]">DTR</span>
            </Check>
            <Check checked={rts} onChange={(v) => (setRts(v), lc("rts", v))}>
              <span className="text-[13px]">RTS</span>
            </Check>
            <ToolBtn title={t("terminal.sendBreak")} onClick={() => lc("break", true)}>
              BRK
            </ToolBtn>
          </>
        )}
        <div className="mx-1 h-4 border-l border-line" />
        <ToolBtn on={!!tab.logPath} title={tab.logPath ? t("terminal.logging", { path: tab.logPath }) : t("terminal.startLog")} onClick={toggleLog}>
          <ScrollText size={14} />
        </ToolBtn>
        {tab.logPath && (
          <button
            className="icon-btn text-[12px]"
            title={t("terminal.openLogFolder")}
            onClick={() => SystemService.OpenPath(tab.logPath.replace(/[\\/][^\\/]+$/, ""))}
          >
            {t("common.open")}
          </button>
        )}
        <ToolBtn title={t("terminal.search") + " (⌘F / Ctrl+Shift+F)"} onClick={() => setSearching(true)}>
          <Search size={14} />
        </ToolBtn>
        <ToolBtn title={t("terminal.saveOutput")} onClick={saveOutput}>
          <FileDown size={14} />
        </ToolBtn>
        <ToolBtn
          title={t("terminal.clear")}
          onClick={() => {
            terminals.get(tab.id)?.term.clear();
            TerminalService.ClearScrollback(tab.id);
          }}
        >
          <Eraser size={14} />
        </ToolBtn>
        {tab.type === "ssh" && (
          <ToolBtn
            on={sftpShown}
            title={t("terminal.showSftp")}
            onClick={() => {
              const st = useStore.getState();
              const open = !sftpShown;
              st.set("sidebar", open ? "sftp" : "sessions");
              if (open) st.set("sidebarOpen", true);
            }}
          >
            <FolderTree size={14} /> SFTP
          </ToolBtn>
        )}
      </div>
      <div className="flex min-h-0 flex-1">
        <div className="relative min-w-0 flex-1 bg-[var(--c-base)]">
          {searching && <SearchBar id={tab.id} onClose={() => setSearching(false)} />}
          <div ref={ref} className="absolute inset-0" onClick={() => terminals.get(tab.id)?.focus()} />
        </div>
      </div>
      {(isSerial || isNet) && <SendBar tab={tab} />}
    </div>
  );
}
