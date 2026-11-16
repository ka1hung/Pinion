import { useEffect, useState } from "react";
import { FolderOpen, Trash2 } from "lucide-react";
import { SystemService, errMsg, type store as S } from "../../lib/api";
import { EditorService } from "../../lib/services";
import { useStore, toast } from "../../lib/store";
import { applyTheme } from "../../lib/actions";
import { Modal, Field, Check } from "../ui";

interface KH {
  line: number;
  hosts: string;
  keyType: string;
  fingerprint: string;
}

export default function SettingsDialog() {
  const set = useStore((s) => s.set);
  const cur = useStore((s) => s.settings);
  const [s, setS] = useState<S.Settings | null>(cur);
  const [tab, setTab] = useState<"general" | "terminal" | "editor" | "files" | "hosts">("general");
  const [kh, setKh] = useState<KH[]>([]);
  const [detected, setDetected] = useState("");
  const [info, setInfo] = useState<any>(null);

  useEffect(() => {
    SystemService.Info().then(setInfo);
    EditorService.DetectExternalEditor().then(setDetected).catch(() => {});
  }, []);
  useEffect(() => {
    if (tab === "hosts") SystemService.KnownHosts().then((k) => setKh(((k ?? []) as KH[]).filter(Boolean)));
  }, [tab]);
  if (!s) return null;
  const up = (p: Partial<S.Settings>) => setS({ ...s, ...p });
  const save = async () => {
    try {
      await SystemService.SaveSettings(s);
      useStore.getState().setSettings(s);
      applyTheme();
      set("settingsOpen", false);
    } catch (e) {
      toast("error", errMsg(e));
    }
  };
  const browse = async (k: "logDir" | "serverRoot") => {
    const p = await SystemService.PickFolder("選擇資料夾");
    if (p) up({ [k]: p } as any);
  };

  const tabs = [
    ["general", "一般"],
    ["terminal", "終端"],
    ["editor", "編輯器"],
    ["files", "Log 與伺服器"],
    ["hosts", "Known hosts"],
  ] as const;

  return (
    <Modal
      title="設定"
      width={680}
      onClose={() => set("settingsOpen", false)}
      footer={
        <>
          <button className="btn" onClick={() => set("settingsOpen", false)}>取消</button>
          <button className="btn-primary" onClick={save}>儲存</button>
        </>
      }
    >
      <div className="flex gap-4">
        <div className="flex w-32 shrink-0 flex-col gap-0.5">
          {tabs.map(([id, label]) => (
            <button key={id} className={"rounded px-2 py-1 text-left " + (tab === id ? "bg-sel" : "hover:bg-hover")} onClick={() => setTab(id)}>
              {label}
            </button>
          ))}
        </div>
        <div className="min-h-[340px] flex-1 space-y-3">
          {tab === "general" && (
            <>
              <Check checked={s.sftpAutoSwitch} onChange={(v) => up({ sftpAutoSwitch: v })}>
                切換分頁時側邊欄自動切換（SSH 分頁顯示 SFTP，其他顯示 Sessions）
              </Check>
              <Field label="主題">
                <select className="input" value={s.theme} onChange={(e) => up({ theme: e.target.value })}>
                  <option value="system">跟隨系統</option>
                  <option value="dark">深色（柔和）</option>
                  <option value="dark-modern">深色（VS Code Dark Modern，較黑）</option>
                  <option value="light">淺色（Light Modern）</option>
                </select>
              </Field>
              {info && (
                <div className="rounded border border-line p-2 text-[13px] text-mute select-text">
                  <div>版本：{info.version}（{info.os}/{info.arch}）</div>
                  <div>設定資料夾：{info.configDir}</div>
                  <div>密碼儲存：{info.osKeychain ? "系統鑰匙圈" : "加密檔（主密碼）"}</div>
                  <div>SSH agent：{info.agent ? "可用" : "未偵測到"}</div>
                  <button className="mt-1 text-accent" onClick={() => SystemService.OpenPath(info.configDir)}>開啟設定資料夾</button>
                </div>
              )}
            </>
          )}
          {tab === "terminal" && (
            <>
              <Field label="字型">
                <input className="input font-mono" value={s.terminalFont} onChange={(e) => up({ terminalFont: e.target.value })} />
              </Field>
              <div className="grid grid-cols-2 gap-3">
                <Field label="字級">
                  <input className="input" type="number" value={s.terminalFontSize} onChange={(e) => up({ terminalFontSize: Number(e.target.value) })} />
                </Field>
                <Field label="捲動緩衝行數">
                  <input className="input" type="number" value={s.scrollback} onChange={(e) => up({ scrollback: Number(e.target.value) })} />
                </Field>
              </div>
              <Check checked={s.copyOnSelect} onChange={(v) => up({ copyOnSelect: v })}>選取即複製</Check>
              <br />
              <Check checked={s.sshCompletionIgnoreCase} onChange={(v) => up({ sshCompletionIgnoreCase: v })}>
                SSH 登入後讓 Tab 補齊忽略大小寫（bash）
              </Check>
              <div className="ml-6 text-[12px] text-mute">
                登入後會自動送出 <code>bind 'set completion-ignore-case on'</code>，終端機會看到這一行。只對 bash 有效；交換器等非 Linux 設備請關閉。
                想永久生效且不顯示，可在設備上執行：<code>echo 'set completion-ignore-case on' &gt;&gt; ~/.inputrc</code>
              </div>
              <br />
              <Check checked={s.cursorBlink} onChange={(v) => up({ cursorBlink: v })}>游標閃爍</Check>
              <br />
              <Check checked={s.smartCopyPaste} onChange={(v) => up({ smartCopyPaste: v })}>
                Ctrl+C 有選取文字時複製（沒選取時照常中斷）、Ctrl+V 貼上（Windows / Linux）
              </Check>
              <Field label="終端機右鍵" className="mt-2">
                <select className="input" value={s.termRightClick || "menu"} onChange={(e) => up({ termRightClick: e.target.value })}>
                  <option value="menu">顯示選單（複製 / 貼上 / 全選 / 搜尋 / 清除）</option>
                  <option value="paste">直接複製或貼上（有選取就複製，否則貼上；PuTTY 習慣）</option>
                </select>
              </Field>
              <div className="text-[13px] text-mute">
                複製：{"Ctrl+C（有選取時）、Ctrl+Shift+C、Ctrl+Insert"}；貼上：{"Ctrl+V、Ctrl+Shift+V、Shift+Insert、滑鼠中鍵"}。macOS 用 ⌘C / ⌘V。
              </div>
            </>
          )}
          {tab === "editor" && (
            <>
              <Field label="字型">
                <input className="input font-mono" value={s.editorFont} onChange={(e) => up({ editorFont: e.target.value })} />
              </Field>
              <div className="grid grid-cols-3 gap-3">
                <Field label="字級">
                  <input className="input" type="number" value={s.editorFontSize} onChange={(e) => up({ editorFontSize: Number(e.target.value) })} />
                </Field>
                <Field label="Tab 寬度">
                  <input className="input" type="number" value={s.editorTabSize} onChange={(e) => up({ editorTabSize: Number(e.target.value) })} />
                </Field>
                <Field label="大檔門檻 (MB)">
                  <input className="input" type="number" value={s.bigFileLimitMB} onChange={(e) => up({ bigFileLimitMB: Number(e.target.value) })} />
                </Field>
              </div>
              <div className="flex gap-4">
                <Check checked={s.editorMinimap} onChange={(v) => up({ editorMinimap: v })}>迷你地圖</Check>
                <Check checked={s.editorWordWrap} onChange={(v) => up({ editorWordWrap: v })}>自動換行</Check>
              </div>
              <div className="border-t border-line pt-3">
                <Check checked={s.useExternalEditor} onChange={(v) => up({ useExternalEditor: v })}>
                  雙擊遠端檔案時預設用外部編輯器
                </Check>
                <Field label="外部編輯器指令（{file} 會換成檔案路徑）" className="mt-2" hint={`留空自動偵測：${detected || "找不到 VS Code，改用系統預設程式"}`}>
                  <input className="input font-mono" value={s.externalEditorCmd} placeholder="code --wait {file}" onChange={(e) => up({ externalEditorCmd: e.target.value })} />
                </Field>
              </div>
            </>
          )}
          {tab === "files" && (
            <>
              <Field label="Session log 資料夾">
                <div className="flex gap-1">
                  <input className="input font-mono" value={s.logDir} onChange={(e) => up({ logDir: e.target.value })} />
                  <button className="btn" onClick={() => browse("logDir")}><FolderOpen size={14} /></button>
                </div>
              </Field>
              <div className="flex gap-4">
                <Check checked={s.logStripAnsi} onChange={(v) => up({ logStripAnsi: v })}>移除 ANSI 色碼</Check>
                <Check checked={s.logTimestamps} onChange={(v) => up({ logTimestamps: v })}>每行加時間戳記</Check>
              </div>
              <Field label="臨時伺服器預設資料夾" className="pt-2">
                <div className="flex gap-1">
                  <input className="input font-mono" value={s.serverRoot} onChange={(e) => up({ serverRoot: e.target.value })} />
                  <button className="btn" onClick={() => browse("serverRoot")}><FolderOpen size={14} /></button>
                </div>
              </Field>
              <Field label="伺服器閒置自動關閉（分鐘，0 = 不關閉）">
                <input className="input w-32" type="number" value={s.serverIdleMinutes} onChange={(e) => up({ serverIdleMinutes: Number(e.target.value) })} />
              </Field>
            </>
          )}
          {tab === "hosts" && (
            <div>
              <div className="mb-2 text-[13px] text-mute">已信任的主機金鑰（OpenSSH known_hosts 格式）。設備重灌後金鑰會變，在這裡刪除舊的再重新連線。</div>
              <div className="max-h-[320px] overflow-auto rounded border border-line">
                {kh.map((k) => (
                  <div key={k.line} className="flex items-center gap-2 border-b border-line px-2 py-1 last:border-0">
                    <div className="min-w-0 flex-1">
                      <div className="truncate">{k.hosts}</div>
                      <div className="truncate font-mono text-[12px] text-mute">{k.keyType} {k.fingerprint}</div>
                    </div>
                    <button
                      className="icon-btn"
                      onClick={() =>
                        SystemService.RemoveKnownHost(k.line)
                          .then(() => SystemService.KnownHosts())
                          .then((x) => setKh(((x ?? []) as KH[]).filter(Boolean)))
                      }
                    >
                      <Trash2 size={14} />
                    </button>
                  </div>
                ))}
                {!kh.length && <div className="p-3 text-mute">（空）</div>}
              </div>
            </div>
          )}
        </div>
      </div>
    </Modal>
  );
}
