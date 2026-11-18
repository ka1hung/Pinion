import { useEffect, useState } from "react";
import { Server, Cpu, MemoryStick, ArrowUp, ArrowDown, Clock, User, HardDrive } from "lucide-react";
import { MonitorService, errMsg, type monitor } from "../lib/api";
import { useT } from "../lib/i18n";
import { fmtBytes } from "./ui";

const INTERVAL_MS = 3000;

/** Small usage meter; turns amber/red when nearly full. */
function Meter({ pct }: { pct: number }) {
  const p = Math.max(0, Math.min(100, pct));
  const color = p >= 90 ? "bg-danger" : p >= 75 ? "bg-warn" : "bg-ok";
  return (
    <span className="inline-block h-1.5 w-8 shrink-0 overflow-hidden rounded-full bg-line">
      <span className={"block h-full " + color} style={{ width: p + "%" }} />
    </span>
  );
}

function Item({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <span className="flex shrink-0 items-center gap-1 whitespace-nowrap" title={title}>
      {children}
    </span>
  );
}

const pct = (used: number, total: number) => (total > 0 ? (used / total) * 100 : 0);
const rate = (n: number) => (n < 0 ? "–" : fmtBytes(Math.round(n)) + "/s");

/**
 * Remote resource bar under an SSH terminal. Polls MonitorService.Sample one
 * request at a time (never overlapping) while mounted; TerminalView only
 * mounts it for connected SSH tabs with the setting on.
 */
export default function MonitorBar({ tabId }: { tabId: string }) {
  const t = useT();
  const [st, setSt] = useState<monitor.Stats | null>(null);
  const [err, setErr] = useState("");

  useEffect(() => {
    let alive = true;
    let timer: number | undefined;
    const tick = async () => {
      if (!document.hidden) {
        try {
          const s = await MonitorService.Sample(tabId);
          if (!alive) return;
          setSt(s);
          setErr("");
          if (s && !s.supported) return; // nothing to poll on this host
        } catch (e) {
          if (!alive) return;
          setErr(errMsg(e));
        }
      }
      if (alive) timer = window.setTimeout(tick, INTERVAL_MS);
    };
    tick();
    return () => {
      alive = false;
      if (timer) clearTimeout(timer);
      MonitorService.Forget(tabId).catch(() => {});
    };
  }, [tabId]);

  let body: React.ReactNode;
  if (err && !st) body = <span className="truncate text-danger">{t("monitor.error", { msg: err })}</span>;
  else if (!st) body = <span>{t("monitor.loading")}</span>;
  else if (!st.supported) body = <span>{t("monitor.unsupported")}</span>;
  else {
    const days = Math.floor(st.uptime / 86400);
    body = (
      <>
        {st.host && (
          <Item title={t("monitor.host")}>
            <Server size={13} /> {st.host}
          </Item>
        )}
        {st.cpu >= 0 && (
          <Item title={t("monitor.cpu", { load: st.load1.toFixed(2) })}>
            <Cpu size={13} /> <Meter pct={st.cpu} /> {st.cpu.toFixed(0)}%
          </Item>
        )}
        {st.memTotal > 0 && (
          <Item
            title={
              t("monitor.mem", { used: fmtBytes(st.memUsed), total: fmtBytes(st.memTotal) }) +
              (st.swapTotal > 0 ? "\n" + t("monitor.swap", { used: fmtBytes(st.swapUsed), total: fmtBytes(st.swapTotal) }) : "")
            }
          >
            <MemoryStick size={13} /> <Meter pct={pct(st.memUsed, st.memTotal)} /> {fmtBytes(st.memUsed)} / {fmtBytes(st.memTotal)}
          </Item>
        )}
        <Item title={t("monitor.up")}>
          <ArrowUp size={13} /> {rate(st.txRate)}
        </Item>
        <Item title={t("monitor.down")}>
          <ArrowDown size={13} /> {rate(st.rxRate)}
        </Item>
        {st.uptime > 0 && (
          <Item title={t("monitor.uptime")}>
            <Clock size={13} /> {days >= 1 ? t("monitor.days", { n: days }) : t("monitor.hours", { n: Math.floor(st.uptime / 3600) })}
          </Item>
        )}
        {st.user && (
          <Item title={t("monitor.user")}>
            <User size={13} /> {st.user}
          </Item>
        )}
        {(st.disks ?? []).map((d) => (
          <Item key={d.mount} title={t("monitor.disk", { mount: d.mount, used: fmtBytes(d.used), total: fmtBytes(d.total) })}>
            <HardDrive size={13} /> {d.mount}: <Meter pct={pct(d.used, d.total)} /> {pct(d.used, d.total).toFixed(0)}%
          </Item>
        ))}
        {err && <span className="truncate text-danger" title={err}>⚠</span>}
      </>
    );
  }

  return (
    <div className="flex h-6 shrink-0 items-center gap-4 overflow-hidden border-t border-line bg-side px-3 text-[12px] text-mute">
      {body}
    </div>
  );
}
