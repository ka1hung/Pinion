import { SessionService, TerminalService, SystemService, errMsg, type store as S, type tabs as T } from "./api";
import { useStore, toast, lastInfo, type TermTab } from "./store";
import { terminals } from "./terminals";
import { TunnelService } from "./services";
import { closeEditor } from "./editor";

export async function refreshTree() {
  const t = await SessionService.Tree();
  useStore.getState().setTree(t.folders ?? [], (t.sessions ?? []).filter(Boolean) as S.Session[]);
}

export async function loadSettings() {
  const s = await SystemService.Settings();
  useStore.getState().setSettings(s);
  applyTheme();
}

export function applyTheme() {
  const t = useStore.getState().settings?.theme || "system";
  const dark = t === "dark" || t === "dark-modern" || (t === "system" && !window.matchMedia("(prefers-color-scheme: light)").matches);
  document.documentElement.classList.toggle("dark", dark);
  document.documentElement.classList.toggle("dark-modern", dark && t === "dark-modern");
  // Windows: make the native title bar match (no-op on macOS/Linux)
  const css = getComputedStyle(document.documentElement);
  SystemService.SetWindowTheme(dark, css.getPropertyValue("--c-side").trim(), css.getPropertyValue("--c-fg").trim()).catch(() => {});
  terminals.all().forEach((x) => x.applySettings());
}

function defaultView(type: string, sess?: S.Session) {
  return { hex: false, timestamp: false, json: (type === "tcp" || type === "udp") && sess?.net?.format === "json" };
}

function addTermTab(info: T.Info, sess?: S.Session) {
  info = lastInfo.get(info.id) ?? info;
  const tab: TermTab = {
    kind: "terminal",
    id: info.id,
    sessionId: info.sessionId,
    title: info.title,
    type: info.type,
    state: info.state,
    message: info.message,
    describe: info.describe,
    logPath: info.logPath,
    sftpOpen: info.type === "ssh" && (sess?.ssh?.sftpAutoOpen ?? true),
    view: defaultView(info.type, sess),
    color: sess?.color,
  };
  terminals.ensure(info.id);
  const st = useStore.getState();
  if (sess?.net?.format === "hex") tab.view.hex = true;
  terminals.get(info.id)!.setView(tab.view);
  st.addTab(tab);
  // In split mode with focus on the right, place the new tab there.
  if (st.split.on && st.split.focus === "right") st.setSplit({ rightId: tab.id });
}

export async function openSession(id: string) {
  try {
    const sess = useStore.getState().sessions.find((s) => s.id === id);
    const info = await TerminalService.Open(id, 120, 32);
    if (info) addTermTab(info, sess);
    if (sess?.type === "ssh" && sess.ssh?.tunnels?.some((t) => t.autoStart)) {
      TunnelService.StartAuto(id).catch((e: any) => toast("error", errMsg(e)));
    }
  } catch (e) {
    toast("error", errMsg(e));
  }
}

export async function openAdhoc(sess: S.Session) {
  try {
    const info = await TerminalService.OpenAdhoc(sess, 120, 32);
    if (info) addTermTab(info, sess);
  } catch (e) {
    toast("error", errMsg(e));
  }
}

export async function openLocal() {
  try {
    const info = await TerminalService.OpenLocal(120, 32);
    if (info) addTermTab(info);
  } catch (e) {
    toast("error", errMsg(e));
  }
}

export async function closeTab(id: string) {
  const st = useStore.getState();
  const tab = st.tabs.find((t) => t.id === id);
  if (!tab) return;
  if (tab.kind === "terminal") {
    terminals.dispose(id);
    st.removeTab(id);
    TerminalService.Close(id).catch(() => {});
    // editors opened from this connection keep their content but can't save remotely anymore
  } else {
    if (!(await closeEditor(tab))) return;
    st.removeTab(id);
  }
}

/** Parse "user@host:port" quick-connect strings (ssh/telnet/serial/tcp). */
export function parseQuick(input: string): S.Session | null {
  const s = input.trim();
  if (!s) return null;
  let m = s.match(/^(ssh|telnet|tcp|udp):\/\/(.+)$/i);
  let proto = "ssh";
  let rest = s;
  if (m) {
    proto = m[1].toLowerCase();
    rest = m[2];
  } else if (/^(COM\d+|\/dev\/)/i.test(s)) {
    return { type: "serial", name: s, folder: "", serial: { port: s, baud: 115200, dataBits: 8, parity: "none", stopBits: "1" } } as any;
  }
  m = rest.match(/^(?:([^@]+)@)?(\[[^\]]+\]|[^:]+)(?::(\d+))?$/);
  if (!m) return null;
  const user = m[1] ?? "";
  const host = m[2].replace(/^\[|\]$/g, "");
  const port = m[3] ? Number(m[3]) : 0;
  const base = { name: "", folder: "", type: proto } as any;
  if (proto === "ssh") base.ssh = { host, port: port || 22, user, auth: "auto", sftpAutoOpen: true };
  if (proto === "telnet") base.telnet = { host, port: port || 23 };
  if (proto === "tcp" || proto === "udp") base.net = { mode: "client", host, port, format: "text" };
  return base;
}
