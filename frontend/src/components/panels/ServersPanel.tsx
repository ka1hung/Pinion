import { useEffect, useState } from "react";
import { Play, Square, FolderOpen, Copy } from "lucide-react";
import { Clipboard } from "@wailsio/runtime";
import { ServerService } from "../../lib/services";
import { on, errMsg, SystemService } from "../../lib/api";
import { toast } from "../../lib/store";
import { Check, fmtBytes } from "../ui";
import { useT } from "../../lib/i18n";

interface Cfg {
  kind: string;
  root: string;
  bindIp: string;
  port: number;
  writable: boolean;
  user: string;
  password: string;
  idleMinutes: number;
  passivePorts: string;
}
interface Status {
  kind: string;
  running: boolean;
  config: Cfg;
  url: string;
  startedAt: number;
  lastActivity: number;
  error: string;
}
interface Iface {
  name: string;
  ip: string;
  label: string;
}
interface LogEntry {
  kind: string;
  time: number;
  remote: string;
  action: string;
  file: string;
  bytes: number;
  message: string;
}

const DEFAULT_PORT: Record<string, number> = { tftp: 69, http: 8080, ftp: 2121 };
const LABEL: Record<string, string> = { tftp: "TFTP", http: "HTTP", ftp: "FTP" };

function ServerCard({ st, ifaces, defRoot }: { st: Status; ifaces: Iface[]; defRoot: string }) {
  const t = useT();
  const [c, setC] = useState<Cfg>(() => ({ ...st.config, root: st.config.root || defRoot, port: st.config.port || DEFAULT_PORT[st.kind] }));
  useEffect(() => {
    if (st.running) setC({ ...st.config });
  }, [st.running]);
  useEffect(() => {
    if (!c.root && defRoot) setC((x) => ({ ...x, root: defRoot }));
  }, [defRoot]);
  const up = (p: Partial<Cfg>) => setC({ ...c, ...p });
  const toggle = () =>
    (st.running ? ServerService.Stop(st.kind) : ServerService.Start({ ...c, kind: st.kind } as any)).catch((e) => toast("error", errMsg(e)));
  const dis = st.running;
  return (
    <div className="mb-2 rounded border border-line bg-base p-2">
      <div className="mb-1.5 flex items-center gap-2">
        <span className={"h-2 w-2 rounded-full " + (st.running ? "bg-ok" : "bg-line")} />
        <span className="font-semibold">{LABEL[st.kind]}</span>
        <button className={"ml-auto shrink-0 " + (st.running ? "btn-danger !px-2 !py-0.5" : "btn-primary !px-2 !py-0.5")} onClick={toggle}>
          {st.running ? <Square size={13} /> : <Play size={13} />} {st.running ? t("servers.stop") : t("servers.start")}
        </button>
      </div>
      {st.running && (
        <button className="mb-1.5 flex w-full min-w-0 items-center gap-1 font-mono text-[12px] text-accent" title={t("servers.copyUrl")} onClick={() => Clipboard.SetText(st.url)}>
          <span className="truncate">{st.url}</span> <Copy size={11} className="shrink-0" />
        </button>
      )}
      <div className="grid grid-cols-3 gap-1.5 text-[13px]">
        <div className="col-span-3 flex gap-1">
          <input className="input font-mono text-[13px]" disabled={dis} value={c.root} onChange={(e) => up({ root: e.target.value })} title={t("servers.sharedFolder")} />
          <button
            className="icon-btn"
            disabled={dis}
            onClick={async () => {
              const p = await ServerService.PickRoot();
              if (p) up({ root: p });
            }}
          >
            <FolderOpen size={14} />
          </button>
          <button className="icon-btn" title={t("servers.openFolder")} onClick={() => c.root && SystemService.OpenPath(c.root)}>
            ↗
          </button>
        </div>
        <select className="input col-span-2 text-[13px]" disabled={dis} value={c.bindIp || "0.0.0.0"} onChange={(e) => up({ bindIp: e.target.value })} title={t("servers.iface")}>
          {ifaces.map((i) => (
            <option key={i.ip + i.name} value={i.ip}>{i.label}</option>
          ))}
        </select>
        <input className="input text-[13px]" disabled={dis} type="number" value={c.port} onChange={(e) => up({ port: Number(e.target.value) })} title={t("servers.port")} />
        {st.kind !== "tftp" && (
          <>
            <input className="input text-[13px]" disabled={dis} placeholder={t("servers.userPlaceholder")} value={c.user} onChange={(e) => up({ user: e.target.value })} />
            <input className="input col-span-2 text-[13px]" disabled={dis} type="password" placeholder={t("servers.password")} value={c.password} onChange={(e) => up({ password: e.target.value })} />
          </>
        )}
        <div className="col-span-3">
          <Check checked={c.writable} onChange={(v) => !dis && up({ writable: v })}>
            <span className="text-[13px]">{t("servers.allowWrite")}</span>
          </Check>
        </div>
      </div>
      {st.error && <div className="mt-1 whitespace-pre-wrap text-[12px] text-danger">{st.error}</div>}
    </div>
  );
}

export default function ServersPanel() {
  const t = useT();
  const [status, setStatus] = useState<Status[]>([]);
  const [ifaces, setIfaces] = useState<Iface[]>([]);
  const [defRoot, setDefRoot] = useState("");
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const load = () => ServerService.Status().then((s) => setStatus(((s ?? []) as Status[]).filter(Boolean)));
  useEffect(() => {
    load();
    ServerService.Interfaces().then((i) => setIfaces(((i ?? []) as Iface[]).filter(Boolean)));
    ServerService.DefaultRoot().then(setDefRoot);
    const offs = [
      on("server:changed", load),
      on<LogEntry>("server:log", (l) => setLogs((x) => [l, ...x].slice(0, 300))),
    ];
    return () => offs.forEach((f) => f());
  }, []);
  return (
    <div className="flex h-full flex-col">
      <div className="overflow-auto p-2">
        {status.map((s) => (
          <ServerCard key={s.kind} st={s} ifaces={ifaces} defRoot={defRoot} />
        ))}
        <div className="text-[12px] text-mute">{t("servers.note")}</div>
      </div>
      <div className="flex min-h-0 flex-1 flex-col border-t border-line">
        <div className="flex items-center px-2 py-1 text-[13px] text-mute">
          {t("servers.log")}
          <button className="ml-auto text-accent" onClick={() => setLogs([])}>{t("servers.clear")}</button>
        </div>
        <div className="min-h-0 flex-1 overflow-auto px-2 font-mono text-[12px]">
          {logs.map((l, i) => (
            <div key={i} className={"truncate " + (l.action === "error" ? "text-danger" : "")} title={l.message}>
              {new Date(l.time).toLocaleTimeString()} {l.kind.toUpperCase()} {l.remote} {l.action} {l.file} {l.bytes ? fmtBytes(l.bytes) : ""} {l.message}
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
