import { useEffect, useState } from "react";
import { FolderOpen } from "lucide-react";
import { ImportService } from "../../lib/services";
import { errMsg } from "../../lib/api";
import { useStore, toast } from "../../lib/store";
import { Modal, Field } from "../ui";
import { typeIcon } from "../Sidebar";
import { useT } from "../../lib/i18n";

export default function ImportDialog() {
  const t = useT();
  const set = useStore((s) => s.set);
  const [paths, setPaths] = useState<string[]>([]);
  const [path, setPath] = useState("");
  const [folder, setFolder] = useState(() => t("importDialog.defaultFolder"));
  const [res, setRes] = useState<any>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    ImportService.DetectPaths().then((p) => {
      const list = (p ?? []) as string[];
      setPaths(list);
      if (list[0]) preview(list[0]);
    });
  }, []);

  const preview = async (p: string) => {
    setPath(p);
    setRes(null);
    try {
      setRes(await ImportService.Preview(p));
    } catch (e) {
      toast("error", errMsg(e));
    }
  };
  const pick = async () => {
    const p = await ImportService.PickFile();
    if (p) preview(p);
  };
  const commit = async () => {
    setBusy(true);
    try {
      const n = await ImportService.Commit(path, folder);
      toast("success", t("importDialog.imported", { n }));
      set("importOpen", false);
    } catch (e) {
      toast("error", errMsg(e));
    } finally {
      setBusy(false);
    }
  };

  const sessions = (res?.sessions ?? []).filter(Boolean);
  const skipped = (res?.skipped ?? []).filter(Boolean);
  return (
    <Modal
      title={t("importDialog.title")}
      width={720}
      onClose={() => set("importOpen", false)}
      footer={
        <>
          <button className="btn" onClick={() => set("importOpen", false)}>{t("common.cancel")}</button>
          <button className="btn-primary" disabled={!sessions.length || busy} onClick={commit}>
            {t("importDialog.importN", { n: sessions.length })}
          </button>
        </>
      }
    >
      <p className="mb-3 text-mute">
        {t("importDialog.intro")}
      </p>
      <Field label={t("importDialog.source")}>
        <div className="flex gap-1">
          <select className="input" value={path} onChange={(e) => preview(e.target.value)}>
            {!path && <option value="">{t("importDialog.notSelected")}</option>}
            {[...new Set([path, ...paths].filter(Boolean))].map((p) => (
              <option key={p} value={p}>{p}</option>
            ))}
          </select>
          <button className="btn" onClick={pick}><FolderOpen size={14} /> {t("importDialog.choose")}</button>
        </div>
      </Field>
      <Field label={t("importDialog.targetFolder")} className="mt-3">
        <input className="input" value={folder} onChange={(e) => setFolder(e.target.value)} />
      </Field>
      {res && (
        <div className="mt-3">
          <div className="mb-1 text-[13px] text-mute">
            {t("importDialog.summary", { encoding: res.encoding, n: sessions.length, skipped: skipped.length })}
          </div>
          {(res.warnings ?? []).map((w: string, i: number) => (
            <div key={i} className="mb-1 text-[13px] text-warn">⚠ {w}</div>
          ))}
          <div className="max-h-64 overflow-auto rounded border border-line text-[13px]">
            {sessions.map((s: any, i: number) => (
              <div key={i} className="flex items-center gap-2 border-b border-line px-2 py-1 last:border-0">
                {typeIcon(s.type)}
                <span>{s.name}</span>
                <span className="ml-auto text-mute">{[folder, s.folder].filter(Boolean).join("/")}</span>
              </div>
            ))}
            {skipped.map((s: any, i: number) => (
              <div key={"k" + i} className="flex items-center gap-2 border-b border-line px-2 py-1 text-mute last:border-0">
                <span className="line-through">{s.name}</span>
                <span className="ml-auto">{s.reason}</span>
              </div>
            ))}
          </div>
        </div>
      )}
    </Modal>
  );
}
