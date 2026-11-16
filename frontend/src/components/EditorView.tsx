import { useEffect, useRef, useState } from "react";
import Editor, { DiffEditor } from "@monaco-editor/react";
import { Lock, ExternalLink, RefreshCw } from "lucide-react";
import "../lib/monaco";
import { monaco, languageFor, languages } from "../lib/monaco";
import { EditorService } from "../lib/services";
import { SystemService, errMsg, isMac } from "../lib/api";
import { useStore, toast, type EditorTab } from "../lib/store";
import { docs, openExternal, type Doc } from "../lib/editor";
import { themeVariant } from "../lib/terminals";
import { Modal } from "./ui";

const uriFor = (d: Doc) => monaco.Uri.parse(`pinion://doc/${d.id}/${encodeURIComponent(d.name)}`);

// dispose models when documents close
window.addEventListener("pinion:doc-closed", (e) => {
  const id = (e as CustomEvent).detail as string;
  monaco.editor.getModels().forEach((m) => m.uri.path.startsWith(`/${id}/`) && m.uri.scheme === "pinion" && m.dispose());
});

const savedVersions = new Map<string, number>();

interface SaveResult {
  ok: boolean;
  conflict: boolean;
  remoteContent: string;
  needSudo: boolean;
  mtime: number;
  size: number;
  message: string;
}

export default function EditorView({ tab }: { tab: EditorTab }) {
  const settings = useStore((s) => s.settings);
  const updateTab = useStore((s) => s.updateTab);
  const [doc, setDoc] = useState<Doc | undefined>(docs.get(tab.docId));
  const [pos, setPos] = useState({ line: 1, col: 1 });
  const [lang, setLang] = useState(() => (doc ? languageFor(doc.name) : "plaintext"));
  const [conflict, setConflict] = useState<{ remote: string; local: string } | null>(null);
  const [sudo, setSudo] = useState<{ pw: string } | null>(null);
  const [saving, setSaving] = useState(false);
  const [charsets, setCharsets] = useState<string[]>([]);
  const edRef = useRef<monaco.editor.IStandaloneCodeEditor | null>(null);

  useEffect(() => {
    SystemService.Charsets().then((c) => setCharsets(c ?? []));
  }, []);

  // create model once per doc
  const model = (() => {
    if (!doc) return null;
    const uri = uriFor(doc);
    let m = monaco.editor.getModel(uri);
    if (!m) {
      m = monaco.editor.createModel(doc.content, languageFor(doc.name), uri);
      m.setEOL(monaco.editor.EndOfLineSequence.LF);
      savedVersions.set(doc.id, m.getAlternativeVersionId());
    }
    return m;
  })();

  const markClean = () => {
    if (model && doc) {
      savedVersions.set(doc.id, model.getAlternativeVersionId());
      updateTab(tab.id, { dirty: false });
    }
  };

  const applySaved = (r: SaveResult, d: Doc, patch: Partial<Doc> = {}) => {
    const nd = { ...d, ...patch, mtime: r.mtime, size: r.size };
    docs.set(d.id, nd);
    setDoc(nd);
    markClean();
    toast("success", `已儲存 ${d.name}`);
  };

  const save = async (force = false, override?: Partial<Doc>) => {
    if (!doc || !model || doc.readOnly) return;
    setSaving(true);
    const d = { ...doc, ...override };
    try {
      const r = (await EditorService.Save({
        docId: d.id,
        content: model.getValue(),
        encoding: d.encoding,
        eol: d.eol,
        force,
      } as any)) as SaveResult;
      if (r.conflict) setConflict({ remote: r.remoteContent, local: model.getValue() });
      else if (r.needSudo) setSudo({ pw: "" });
      else if (r.ok) applySaved(r, d, override);
      else toast("error", r.message || "儲存失敗");
    } catch (e) {
      toast("error", errMsg(e));
    } finally {
      setSaving(false);
    }
  };

  const saveSudo = async (pw: string) => {
    if (!doc || !model) return;
    setSudo(null);
    try {
      const r = (await EditorService.SaveSudo(doc.id, model.getValue(), doc.encoding, doc.eol, pw)) as SaveResult;
      if (r.ok) applySaved(r, doc);
      else toast("error", r.message || "sudo 存檔失敗");
    } catch (e) {
      toast("error", errMsg(e));
    }
  };

  const reload = async (encoding: string) => {
    if (!doc || !model) return;
    try {
      const d = (await EditorService.Reload(doc.id, encoding)) as Doc;
      docs.set(d.id, d);
      setDoc(d);
      model.setValue(d.content);
      markClean();
    } catch (e) {
      toast("error", errMsg(e));
    }
  };

  const saveRef = useRef(save);
  saveRef.current = save;

  if (!doc || !model) return <div className="p-4 text-mute">文件已關閉</div>;

  const tv = themeVariant();
  const theme = tv === "light" ? "light-modern" : tv === "modern" ? "dark-modern" : "dark-dim";
  const host = tab.path.includes(":") && doc.source === "remote" ? tab.path.split(":")[0] : "";

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex h-8 shrink-0 items-center gap-2 border-b border-line px-3 text-[13px] text-mute">
        {doc.readOnly && <Lock size={13} className="text-warn" />}
        <span className="truncate font-mono">{host ? `${host}:${doc.path}` : doc.path}</span>
        <div className="flex-1" />
        {doc.source === "remote" && (
          <button className="icon-btn flex items-center gap-1 text-[13px]" title="改用外部編輯器" onClick={() => openExternal(doc.tabId, doc.path)}>
            <ExternalLink size={13} /> 外部編輯器
          </button>
        )}
        <button className="icon-btn flex items-center gap-1 text-[13px]" title="從來源重新載入" onClick={() => reload(doc.encoding)}>
          <RefreshCw size={13} /> 重新載入
        </button>
        <button className="btn-primary !py-0.5 text-[13px]" disabled={doc.readOnly || saving || !tab.dirty} onClick={() => save()}>
          儲存 {isMac ? "⌘S" : "Ctrl+S"}
        </button>
      </div>
      <div className="min-h-0 flex-1">
        <Editor
          theme={theme}
          path={model.uri.toString()}
          language={lang}
          onMount={(ed) => {
            edRef.current = ed;
            ed.setModel(model);
            ed.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyS, () => saveRef.current());
            ed.onDidChangeCursorPosition((e) => setPos({ line: e.position.lineNumber, col: e.position.column }));
            ed.onDidChangeModelContent(() => {
              const dirty = model.getAlternativeVersionId() !== savedVersions.get(doc.id);
              const cur = useStore.getState().tabs.find((t) => t.id === tab.id);
              if (cur?.kind === "editor" && cur.dirty !== dirty) updateTab(tab.id, { dirty });
            });
            ed.focus();
          }}
          options={{
            readOnly: doc.readOnly,
            fontFamily: settings?.editorFont,
            fontSize: settings?.editorFontSize || 14,
            tabSize: settings?.editorTabSize || 4,
            minimap: { enabled: settings?.editorMinimap ?? true },
            wordWrap: settings?.editorWordWrap ? "on" : "off",
            automaticLayout: true,
            renderWhitespace: "selection",
            scrollBeyondLastLine: false,
            unicodeHighlight: { ambiguousCharacters: false },
          }}
        />
      </div>
      <div className="flex h-6 shrink-0 items-center gap-3 border-t border-line bg-side px-3 text-[13px] text-mute">
        <span>
          行 {pos.line}，欄 {pos.col}
        </span>
        <div className="flex-1" />
        <select
          className="status-select"
          value={doc.encoding}
          title="編碼：選擇後可「以此編碼重新開啟」或「以此編碼儲存」"
          onChange={async (e) => {
            const enc = e.target.value;
            const reopen = !tab.dirty;
            if (reopen) reload(enc);
            else {
              const nd = { ...doc, encoding: enc };
              docs.set(doc.id, nd);
              setDoc(nd);
              updateTab(tab.id, { dirty: true });
              toast("info", `下次存檔會以 ${enc} 編碼寫入`);
            }
          }}
        >
          {[...new Set([doc.encoding, ...charsets])].map((c) => (
            <option key={c} value={c}>
              {c}
            </option>
          ))}
        </select>
        <select
          className="status-select"
          value={doc.eol}
          title="換行字元"
          onChange={(e) => {
            const nd = { ...doc, eol: e.target.value };
            docs.set(doc.id, nd);
            setDoc(nd);
            updateTab(tab.id, { dirty: true });
          }}
        >
          <option value="LF">LF</option>
          <option value="CRLF">CRLF</option>
        </select>
        <select
          className="status-select"
          value={lang}
          onChange={(e) => {
            setLang(e.target.value);
            monaco.editor.setModelLanguage(model, e.target.value);
          }}
        >
          {languages().map((l) => (
            <option key={l} value={l}>
              {l}
            </option>
          ))}
        </select>
      </div>

      {conflict && (
        <Modal
          title="遠端檔案已被修改"
          width={1000}
          onClose={() => setConflict(null)}
          footer={
            <>
              <button className="btn" onClick={() => setConflict(null)}>
                取消
              </button>
              <button
                className="btn"
                onClick={() => {
                  setConflict(null);
                  reload(doc.encoding);
                }}
              >
                放棄我的變更，載入遠端版本
              </button>
              <button
                className="btn-danger"
                onClick={() => {
                  setConflict(null);
                  save(true);
                }}
              >
                覆寫遠端
              </button>
            </>
          }
        >
          <div className="mb-2 text-mute">左：目前遠端內容　右：你的版本。開檔後有人改過這個檔案。</div>
          <div className="h-[60vh] border border-line">
            <DiffEditor
              theme={theme}
              original={conflict.remote}
              modified={conflict.local}
              language={lang}
              options={{ readOnly: true, renderSideBySide: true, automaticLayout: true }}
            />
          </div>
        </Modal>
      )}

      {sudo && (
        <Modal
          title="權限不足"
          width={460}
          onClose={() => setSudo(null)}
          footer={
            <>
              <button className="btn" onClick={() => setSudo(null)}>
                取消
              </button>
              <button className="btn-primary" onClick={() => saveSudo(sudo.pw)}>
                用 sudo 存檔
              </button>
            </>
          }
        >
          <p className="mb-2">沒有寫入「{doc.path}」的權限。要先上傳到暫存位置，再用 sudo 搬回原路徑嗎？</p>
          <input
            autoFocus
            type="password"
            className="input"
            placeholder="sudo 密碼（留空使用此 session 已存的密碼）"
            value={sudo.pw}
            onChange={(e) => setSudo({ pw: e.target.value })}
            onKeyDown={(e) => e.key === "Enter" && saveSudo(sudo.pw)}
          />
        </Modal>
      )}
    </div>
  );
}
