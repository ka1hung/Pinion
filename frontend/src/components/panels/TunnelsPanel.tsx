import { useEffect, useState } from "react";
import { Play, Square, Pencil } from "lucide-react";
import { TunnelService } from "../../lib/services";
import { on, errMsg } from "../../lib/api";
import { useStore, toast } from "../../lib/store";
import { fmtBytes } from "../ui";
import { useT } from "../../lib/i18n";

interface Row {
  sessionId: string;
  sessionName: string;
  folder: string;
  tunnel: { id: string; kind: string; bindHost: string; bindPort: number; targetHost?: string; targetPort?: number; autoStart?: boolean };
  running: boolean;
  error: string;
  conns: number;
  bytesIn: number;
  bytesOut: number;
}

function describe(t: Row["tunnel"], tr: ReturnType<typeof useT>) {
  const bind = `${t.bindHost || "127.0.0.1"}:${t.bindPort}`;
  if (t.kind === "D") return `SOCKS5 ${bind}`;
  if (t.kind === "R") return tr("tunnels.remoteToLocal", { bind, target: `${t.targetHost}:${t.targetPort}` });
  return `${bind} → ${t.targetHost}:${t.targetPort}`;
}

export default function TunnelsPanel() {
  const t = useT();
  const [rows, setRows] = useState<Row[]>([]);
  const { sessions, set } = useStore();
  const load = () => TunnelService.List().then((r) => setRows(((r ?? []) as Row[]).filter(Boolean)));
  useEffect(() => {
    load();
    const off = on("tunnel:changed", load);
    return off;
  }, [sessions]);
  const toggle = (r: Row) =>
    (r.running ? TunnelService.Stop(r.sessionId, r.tunnel.id) : TunnelService.Start(r.sessionId, r.tunnel.id))
      .catch((e) => toast("error", errMsg(e)))
      .finally(load);

  const groups = new Map<string, Row[]>();
  rows.forEach((r) => groups.set(r.sessionId, [...(groups.get(r.sessionId) ?? []), r]));

  return (
    <div className="h-full overflow-auto p-2">
      {!rows.length && (
        <div className="p-3 text-center text-mute">
          {t("tunnels.empty")}
        </div>
      )}
      {[...groups.entries()].map(([sid, list]) => (
        <div key={sid} className="mb-3">
          <div className="mb-1 flex items-center gap-1 text-[13px] text-mute">
            <span className="truncate">{list[0].folder ? list[0].folder + " / " : ""}{list[0].sessionName}</span>
            <button
              className="icon-btn ml-auto"
              title={t("common.edit")}
              onClick={() => {
                const s = sessions.find((x) => x.id === sid);
                if (s) set("sessionEditor", { session: s });
              }}
            >
              <Pencil size={13} />
            </button>
          </div>
          {list.map((r) => (
            <div key={r.tunnel.id} className="mb-1 rounded border border-line bg-base p-2">
              <div className="flex items-center gap-2">
                <span className={"rounded px-1 text-[12px] font-bold " + (r.running ? "bg-ok text-white" : "bg-panel text-mute")}>{r.tunnel.kind}</span>
                <span className="min-w-0 flex-1 truncate font-mono text-[13px]">{describe(r.tunnel, t)}</span>
                <button className="icon-btn" title={r.running ? t("tunnels.stop") : t("tunnels.start")} onClick={() => toggle(r)}>
                  {r.running ? <Square size={14} className="text-danger" /> : <Play size={14} className="text-ok" />}
                </button>
              </div>
              {r.running && (
                <div className="mt-1 text-[12px] text-mute">
                  {t("tunnels.conns", { count: r.conns })} · ↑ {fmtBytes(r.bytesOut)} · ↓ {fmtBytes(r.bytesIn)}
                </div>
              )}
              {r.error && <div className="mt-1 text-[12px] text-danger">{r.error}</div>}
            </div>
          ))}
        </div>
      ))}
    </div>
  );
}
