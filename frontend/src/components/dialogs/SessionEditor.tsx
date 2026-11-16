import { useEffect, useState } from "react";
import { RefreshCw, Plus, Trash2, FolderOpen } from "lucide-react";
import { SessionService, SystemService, errMsg, type store as S } from "../../lib/api";
import { SerialService } from "../../lib/services";
import { useStore, toast, type SessionEditorState } from "../../lib/store";
import { openSession } from "../../lib/actions";
import { Modal, Field, Check } from "../ui";

const TYPES = [
  { id: "ssh", label: "SSH" },
  { id: "serial", label: "Serial" },
  { id: "telnet", label: "Telnet" },
  { id: "tcp", label: "TCP" },
  { id: "udp", label: "UDP" },
  { id: "local", label: "本機 Shell" },
];
const BAUDS = [300, 1200, 2400, 4800, 9600, 19200, 38400, 57600, 115200, 230400, 460800, 921600];
const COLORS = ["", "#e5534b", "#f0883e", "#d4a72c", "#3fb950", "#2f81f7", "#a371f7", "#db61a2"];

function blank(type: string, folder = ""): S.Session {
  const s: any = { id: "", name: "", folder, type };
  if (type === "ssh") s.ssh = { host: "", port: 22, user: "", auth: "auto", sftpAutoOpen: true, keepAlive: 30, autoReconnect: false, jumpHosts: [], tunnels: [] };
  if (type === "serial") s.serial = { port: "", baud: 115200, dataBits: 8, parity: "none", stopBits: "1", sendLineEnd: "cr", localEcho: false };
  if (type === "telnet") s.telnet = { host: "", port: 23 };
  if (type === "tcp" || type === "udp") s.net = { mode: "client", host: "", port: 0, format: "text", appendLine: "none", localEcho: false };
  if (type === "local") s.local = { shell: "", args: [], cwd: "" };
  return s;
}

interface Port {
  name: string;
  display: string;
}

export default function SessionEditor({ init }: { init: SessionEditorState }) {
  const { set, folders, sessions } = useStore();
  const [s, setS] = useState<S.Session>(() =>
    init.session ? JSON.parse(JSON.stringify(init.session)) : blank(init.type ?? "ssh", init.folder ?? ""),
  );
  const [password, setPassword] = useState("");
  const [passphrase, setPassphrase] = useState("");
  const [hasPw, setHasPw] = useState(false);
  const [hasPp, setHasPp] = useState(false);
  const [ports, setPorts] = useState<Port[]>([]);
  const [charsets, setCharsets] = useState<string[]>([]);
  const [tab, setTab] = useState<"main" | "advanced" | "tunnels">("main");
  const isNew = !s.id;

  const loadPorts = () =>
    SerialService.ListPorts()
      .then((p) => setPorts(((p ?? []) as any[]).filter(Boolean)))
      .catch((e) => toast("error", errMsg(e)));

  useEffect(() => {
    SystemService.Charsets().then((c) => setCharsets(c ?? []));
    if (s.id) {
      SessionService.HasSecret(s.id, "password").then(setHasPw);
      SessionService.HasSecret(s.id, "passphrase").then(setHasPp);
    }
  }, []);
  useEffect(() => {
    if (s.type === "serial") loadPorts();
  }, [s.type]);

  const up = (patch: Partial<S.Session>) => setS((x) => ({ ...x, ...patch }));
  const upSSH = (patch: Partial<S.SSHConfig>) => setS((x) => ({ ...x, ssh: { ...x.ssh!, ...patch } }));
  const upSerial = (patch: Partial<S.SerialConfig>) => setS((x) => ({ ...x, serial: { ...x.serial!, ...patch } }));
  const upNet = (patch: Partial<S.NetConfig>) => setS((x) => ({ ...x, net: { ...x.net!, ...patch } }));

  const changeType = (type: string) => {
    if (type === s.type) return;
    const b = blank(type, s.folder);
    setS({ ...b, id: s.id, name: s.name, folder: s.folder, notes: s.notes, color: s.color, charset: s.charset, logToFile: s.logToFile } as any);
  };

  const autoName = () => {
    if (s.name.trim()) return s.name.trim();
    if (s.ssh) return s.ssh.user ? `${s.ssh.user}@${s.ssh.host}` : s.ssh.host;
    if (s.telnet) return s.telnet.host;
    if (s.serial) return s.serial.port;
    if (s.net) return `${s.type.toUpperCase()} ${s.net.host || "*"}:${s.net.port}`;
    return "Local";
  };

  const save = async (connect: boolean) => {
    const name = autoName();
    if (!name) return toast("error", "請填寫名稱或主機");
    if (s.ssh && !s.ssh.host) return toast("error", "請填寫主機");
    if (s.serial && !s.serial.port) return toast("error", "請選擇 Serial 埠");
    try {
      // timestamps are owned by the backend; an empty string would not parse as time.Time
      const { createdAt: _c, updatedAt: _u, ...rest } = s as any;
      const saved = await SessionService.Save({ ...rest, name } as S.Session);
      if (!saved) return;
      if (password) await SessionService.SetSecret(saved.id, "password", password);
      if (passphrase) await SessionService.SetSecret(saved.id, "passphrase", passphrase);
      set("sessionEditor", null);
      if (connect) setTimeout(() => openSession(saved.id), 50);
    } catch (e) {
      toast("error", errMsg(e));
    }
  };

  const sshSessions = sessions.filter((x) => x.type === "ssh" && x.id !== s.id);
  const pickKey = async () => {
    const p = await SystemService.PickFile("選擇私鑰", []);
    if (p) upSSH({ keyPath: p });
  };

  return (
    <Modal
      title={isNew ? "新增 Session" : `編輯「${s.name}」`}
      onClose={() => set("sessionEditor", null)}
      width={640}
      closeOnBackdrop={false}
      footer={
        <>
          <button className="btn" onClick={() => set("sessionEditor", null)}>
            取消
          </button>
          <button className="btn" onClick={() => save(false)}>
            儲存
          </button>
          <button className="btn-primary" onClick={() => save(true)}>
            儲存並連線
          </button>
        </>
      }
    >
      <div className="mb-3 flex gap-1">
        {TYPES.map((t) => (
          <button
            key={t.id}
            className={"rounded px-3 py-1 " + (s.type === t.id ? "bg-accent text-accentfg" : "border border-line hover:bg-hover")}
            onClick={() => changeType(t.id)}
          >
            {t.label}
          </button>
        ))}
      </div>

      <div className="mb-3 flex gap-4 border-b border-line text-[13px]">
        {(["main", "advanced", ...(s.type === "ssh" ? ["tunnels"] : [])] as const).map((t) => (
          <button key={t} className={"pb-1 " + (tab === t ? "border-b-2 border-accent text-fg" : "text-mute")} onClick={() => setTab(t as any)}>
            {t === "main" ? "連線" : t === "advanced" ? "進階" : `隧道 (${s.ssh?.tunnels?.length ?? 0})`}
          </button>
        ))}
      </div>

      <div className="min-h-[400px]">
      {tab === "main" && (
        <div className="grid grid-cols-6 gap-3">
          <Field label="名稱" className="col-span-3">
            <input className="input" value={s.name} placeholder={autoName() || "自動"} onChange={(e) => up({ name: e.target.value })} />
          </Field>
          <Field label="資料夾（案場）" className="col-span-3">
            <input className="input" list="pinion-folders" value={s.folder} placeholder="例如 台北廠/機房A" onChange={(e) => up({ folder: e.target.value })} />
            <datalist id="pinion-folders">
              {folders.map((f) => (
                <option key={f} value={f} />
              ))}
            </datalist>
          </Field>

          {s.ssh && (
            <>
              <Field label="主機" className="col-span-3">
                <input autoFocus className="input" value={s.ssh.host} onChange={(e) => upSSH({ host: e.target.value.trim() })} />
              </Field>
              <Field label="Port" className="col-span-1">
                <input className="input" type="number" value={s.ssh.port} onChange={(e) => upSSH({ port: Number(e.target.value) })} />
              </Field>
              <Field label="使用者" className="col-span-2">
                <input className="input" value={s.ssh.user} onChange={(e) => upSSH({ user: e.target.value.trim() })} />
              </Field>
              <Field label="驗證方式" className="col-span-2">
                <select className="input" value={s.ssh.auth} onChange={(e) => upSSH({ auth: e.target.value })}>
                  <option value="auto">自動（agent → 金鑰 → 密碼）</option>
                  <option value="password">密碼</option>
                  <option value="key">私鑰檔</option>
                  <option value="agent">SSH agent</option>
                </select>
              </Field>
              {(s.ssh.auth === "password" || s.ssh.auth === "auto") && (
                <Field label="密碼（存進系統鑰匙圈）" className="col-span-4" hint={hasPw ? "已儲存密碼；留空保持不變" : "留空則連線時詢問"}>
                  <div className="flex gap-1">
                    <input className="input" type="password" value={password} placeholder={hasPw ? "••••••••" : ""} onChange={(e) => setPassword(e.target.value)} />
                    {hasPw && (
                      <button className="btn" onClick={() => SessionService.ClearSecret(s.id, "password").then(() => setHasPw(false))}>
                        清除
                      </button>
                    )}
                  </div>
                </Field>
              )}
              {(s.ssh.auth === "key" || s.ssh.auth === "auto") && (
                <>
                  <Field label="私鑰檔" className="col-span-4" hint={s.ssh.auth === "auto" ? "留空會嘗試 ~/.ssh/id_ed25519、id_ecdsa、id_rsa" : undefined}>
                    <div className="flex gap-1">
                      <input className="input font-mono" value={s.ssh.keyPath ?? ""} onChange={(e) => upSSH({ keyPath: e.target.value })} />
                      <button className="btn" onClick={pickKey}>
                        <FolderOpen size={14} />
                      </button>
                    </div>
                  </Field>
                  <Field label="金鑰密語" className="col-span-2" hint={hasPp ? "已儲存" : undefined}>
                    <input className="input" type="password" value={passphrase} placeholder={hasPp ? "••••••" : ""} onChange={(e) => setPassphrase(e.target.value)} />
                  </Field>
                </>
              )}
              <div className="col-span-6">
                <div className="label flex items-center">
                  跳板機（ProxyJump，依序經過）
                  <button
                    className="icon-btn ml-auto"
                    onClick={() => upSSH({ jumpHosts: [...(s.ssh!.jumpHosts ?? []), { sessionId: sshSessions[0]?.id ?? "", host: "", port: 22, user: "" }] })}
                  >
                    <Plus size={14} />
                  </button>
                </div>
                {(s.ssh.jumpHosts ?? []).map((j, i) => {
                  const upJ = (p: Partial<S.JumpHost>) =>
                    upSSH({ jumpHosts: s.ssh!.jumpHosts!.map((x, k) => (k === i ? { ...x, ...p } : x)) });
                  return (
                    <div key={i} className="mb-1 flex items-center gap-1">
                      <span className="w-5 text-mute">{i + 1}.</span>
                      <select className="input w-48" value={j.sessionId ?? ""} onChange={(e) => upJ({ sessionId: e.target.value })}>
                        <option value="">手動輸入…</option>
                        {sshSessions.map((x) => (
                          <option key={x.id} value={x.id}>
                            {x.folder ? x.folder + " / " : ""}
                            {x.name}
                          </option>
                        ))}
                      </select>
                      {!j.sessionId && (
                        <>
                          <input className="input" placeholder="user" value={j.user ?? ""} onChange={(e) => upJ({ user: e.target.value })} />
                          <input className="input" placeholder="host" value={j.host ?? ""} onChange={(e) => upJ({ host: e.target.value })} />
                          <input className="input w-20" type="number" value={j.port ?? 22} onChange={(e) => upJ({ port: Number(e.target.value) })} />
                        </>
                      )}
                      <button className="icon-btn" onClick={() => upSSH({ jumpHosts: s.ssh!.jumpHosts!.filter((_, k) => k !== i) })}>
                        <Trash2 size={14} />
                      </button>
                    </div>
                  );
                })}
              </div>
              <div className="col-span-6 flex flex-wrap gap-4">
                <Check checked={s.ssh.sftpAutoOpen} onChange={(v) => upSSH({ sftpAutoOpen: v })}>
                  自動開啟 SFTP 面板
                </Check>
                <Check checked={!!s.ssh.autoReconnect} onChange={(v) => upSSH({ autoReconnect: v })}>
                  斷線自動重連
                </Check>
              </div>
            </>
          )}

          {s.serial && (
            <>
              <Field label="埠" className="col-span-5">
                <div className="flex gap-1">
                  <select className="input" value={s.serial.port} onChange={(e) => upSerial({ port: e.target.value })}>
                    <option value="">選擇…</option>
                    {s.serial.port && !ports.some((p) => p.name === s.serial!.port) && <option value={s.serial.port}>{s.serial.port}（未連接）</option>}
                    {ports.map((p) => (
                      <option key={p.name} value={p.name}>
                        {p.display || p.name}
                      </option>
                    ))}
                  </select>
                  <button className="btn" title="重新掃描" onClick={loadPorts}>
                    <RefreshCw size={14} />
                  </button>
                </div>
              </Field>
              <Field label="Baud" className="col-span-1">
                <input className="input" list="pinion-bauds" value={s.serial.baud} onChange={(e) => upSerial({ baud: Number(e.target.value) || 0 })} />
                <datalist id="pinion-bauds">
                  {BAUDS.map((b) => (
                    <option key={b} value={b} />
                  ))}
                </datalist>
              </Field>
              <Field label="Data bits" className="col-span-2">
                <select className="input" value={s.serial.dataBits} onChange={(e) => upSerial({ dataBits: Number(e.target.value) })}>
                  {[8, 7, 6, 5].map((b) => (
                    <option key={b}>{b}</option>
                  ))}
                </select>
              </Field>
              <Field label="Parity" className="col-span-2">
                <select className="input" value={s.serial.parity} onChange={(e) => upSerial({ parity: e.target.value })}>
                  {["none", "odd", "even", "mark", "space"].map((p) => (
                    <option key={p}>{p}</option>
                  ))}
                </select>
              </Field>
              <Field label="Stop bits" className="col-span-2">
                <select className="input" value={s.serial.stopBits} onChange={(e) => upSerial({ stopBits: e.target.value })}>
                  {["1", "1.5", "2"].map((p) => (
                    <option key={p}>{p}</option>
                  ))}
                </select>
              </Field>
              <Field label="Enter 送出" className="col-span-2">
                <select className="input" value={s.serial.sendLineEnd ?? "cr"} onChange={(e) => upSerial({ sendLineEnd: e.target.value })}>
                  <option value="cr">CR (\r)</option>
                  <option value="lf">LF (\n)</option>
                  <option value="crlf">CRLF</option>
                </select>
              </Field>
              <div className="col-span-4 flex items-end gap-4 pb-1">
                <Check checked={!!s.serial.localEcho} onChange={(v) => upSerial({ localEcho: v })}>
                  本機回顯
                </Check>
                <Check checked={s.serial.dtr ?? true} onChange={(v) => upSerial({ dtr: v })}>
                  DTR
                </Check>
                <Check checked={s.serial.rts ?? true} onChange={(v) => upSerial({ rts: v })}>
                  RTS
                </Check>
              </div>
            </>
          )}

          {s.telnet && (
            <>
              <Field label="主機" className="col-span-4">
                <input autoFocus className="input" value={s.telnet.host} onChange={(e) => up({ telnet: { ...s.telnet!, host: e.target.value.trim() } })} />
              </Field>
              <Field label="Port" className="col-span-2">
                <input className="input" type="number" value={s.telnet.port} onChange={(e) => up({ telnet: { ...s.telnet!, port: Number(e.target.value) } })} />
              </Field>
            </>
          )}

          {s.net && (
            <>
              <Field label="模式" className="col-span-2">
                <select className="input" value={s.net.mode} onChange={(e) => upNet({ mode: e.target.value })}>
                  <option value="client">Client（連到對方）</option>
                  <option value="server">Server（等待連入）</option>
                </select>
              </Field>
              <Field label={s.net.mode === "server" ? "綁定位址（空白 = 全部）" : "主機"} className="col-span-3">
                <input className="input" value={s.net.host} placeholder={s.net.mode === "server" ? "0.0.0.0" : ""} onChange={(e) => upNet({ host: e.target.value.trim() })} />
              </Field>
              <Field label="Port" className="col-span-1">
                <input className="input" type="number" value={s.net.port || ""} onChange={(e) => upNet({ port: Number(e.target.value) })} />
              </Field>
              <Field label="預設格式" className="col-span-2">
                <select className="input" value={s.net.format ?? "text"} onChange={(e) => upNet({ format: e.target.value })}>
                  <option value="text">UTF-8 文字</option>
                  <option value="json">JSON</option>
                  <option value="hex">HEX</option>
                </select>
              </Field>
              <Field label="送出時附加" className="col-span-2">
                <select className="input" value={s.net.appendLine ?? "none"} onChange={(e) => upNet({ appendLine: e.target.value })}>
                  <option value="none">無</option>
                  <option value="lf">LF</option>
                  <option value="crlf">CRLF</option>
                </select>
              </Field>
              <div className="col-span-2 flex items-end pb-1">
                <Check checked={!!s.net.localEcho} onChange={(v) => upNet({ localEcho: v })}>
                  本機回顯
                </Check>
              </div>
            </>
          )}

          {s.local && (
            <>
              <Field label="Shell（空白 = 系統預設）" className="col-span-4">
                <input className="input font-mono" value={s.local.shell ?? ""} placeholder="/bin/zsh、powershell.exe、wsl.exe…" onChange={(e) => up({ local: { ...s.local!, shell: e.target.value } })} />
              </Field>
              <Field label="起始目錄" className="col-span-2">
                <input className="input font-mono" value={s.local.cwd ?? ""} onChange={(e) => up({ local: { ...s.local!, cwd: e.target.value } })} />
              </Field>
            </>
          )}
        </div>
      )}

      {tab === "advanced" && (
        <div className="grid grid-cols-6 gap-3">
          {s.ssh && (
            <>
              <Field label="開機後執行指令" className="col-span-4">
                <input className="input font-mono" value={s.ssh.startupCommand ?? ""} onChange={(e) => upSSH({ startupCommand: e.target.value })} />
              </Field>
              <Field label="Keepalive（秒）" className="col-span-2">
                <input className="input" type="number" value={s.ssh.keepAlive ?? 30} onChange={(e) => upSSH({ keepAlive: Number(e.target.value) })} />
              </Field>
              <Field label="TERM" className="col-span-2">
                <input className="input" value={s.ssh.termType ?? ""} placeholder="xterm-256color" onChange={(e) => upSSH({ termType: e.target.value })} />
              </Field>
            </>
          )}
          {s.type !== "local" && (
            <Field label="終端編碼" className="col-span-2" hint="舊設備常用 Big5">
              <select className="input" value={s.charset || "UTF-8"} onChange={(e) => up({ charset: e.target.value === "UTF-8" ? "" : e.target.value })}>
                {charsets.filter((c) => c !== "UTF-8 BOM").map((c) => (
                  <option key={c}>{c}</option>
                ))}
              </select>
            </Field>
          )}
          <Field label="標籤顏色" className="col-span-2">
            <div className="flex gap-1 pt-1">
              {COLORS.map((c) => (
                <button
                  key={c || "none"}
                  className={"h-5 w-5 rounded-full border " + (s.color === c || (!s.color && !c) ? "border-fg" : "border-line")}
                  style={{ background: c || "transparent" }}
                  onClick={() => up({ color: c })}
                />
              ))}
            </div>
          </Field>
          <div className="col-span-6">
            <Check checked={!!s.logToFile} onChange={(v) => up({ logToFile: v })}>
              自動存 Session log（檔名含案場與時間；可能記錄到密碼等敏感內容）
            </Check>
          </div>
          <Field label="備註" className="col-span-6">
            <textarea className="input h-20" value={s.notes ?? ""} onChange={(e) => up({ notes: e.target.value })} />
          </Field>
        </div>
      )}

      {tab === "tunnels" && s.ssh && (
        <div>
          <div className="mb-2 text-[13px] text-mute">
            L：本機埠轉到遠端可達的目標　R：遠端埠轉回本機目標　D：本機 SOCKS5 代理
          </div>
          <table className="w-full text-[13px]">
            <thead className="text-mute">
              <tr>
                <th className="text-left font-normal">類型</th>
                <th className="text-left font-normal">綁定位址</th>
                <th className="text-left font-normal">綁定埠</th>
                <th className="text-left font-normal">目標主機</th>
                <th className="text-left font-normal">目標埠</th>
                <th className="font-normal">自動</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {(s.ssh.tunnels ?? []).map((t, i) => {
                const upT = (p: Partial<S.Tunnel>) => upSSH({ tunnels: s.ssh!.tunnels!.map((x, k) => (k === i ? { ...x, ...p } : x)) });
                return (
                  <tr key={i}>
                    <td className="pr-1">
                      <select className="input" value={t.kind} onChange={(e) => upT({ kind: e.target.value })}>
                        <option value="L">L 本機</option>
                        <option value="R">R 遠端</option>
                        <option value="D">D SOCKS</option>
                      </select>
                    </td>
                    <td className="pr-1">
                      <input className="input" value={t.bindHost} onChange={(e) => upT({ bindHost: e.target.value })} />
                    </td>
                    <td className="pr-1">
                      <input className="input w-20" type="number" value={t.bindPort || ""} onChange={(e) => upT({ bindPort: Number(e.target.value) })} />
                    </td>
                    <td className="pr-1">
                      <input className="input" disabled={t.kind === "D"} value={t.targetHost ?? ""} onChange={(e) => upT({ targetHost: e.target.value })} />
                    </td>
                    <td className="pr-1">
                      <input className="input w-20" type="number" disabled={t.kind === "D"} value={t.targetPort || ""} onChange={(e) => upT({ targetPort: Number(e.target.value) })} />
                    </td>
                    <td className="text-center">
                      <input type="checkbox" className="chk" checked={!!t.autoStart} onChange={(e) => upT({ autoStart: e.target.checked })} />
                    </td>
                    <td>
                      <button className="icon-btn" onClick={() => upSSH({ tunnels: s.ssh!.tunnels!.filter((_, k) => k !== i) })}>
                        <Trash2 size={14} />
                      </button>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
          <button
            className="btn mt-2"
            onClick={() => upSSH({ tunnels: [...(s.ssh!.tunnels ?? []), { id: "", kind: "L", bindHost: "127.0.0.1", bindPort: 0, targetHost: "127.0.0.1", targetPort: 0, autoStart: false }] })}
          >
            <Plus size={14} /> 新增隧道
          </button>
        </div>
      )}
      </div>
    </Modal>
  );
}
