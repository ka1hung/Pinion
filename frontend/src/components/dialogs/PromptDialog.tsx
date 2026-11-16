import { useEffect, useState } from "react";
import { ShieldAlert, KeyRound } from "lucide-react";
import { SystemService, type prompt as P } from "../../lib/api";
import { useStore } from "../../lib/store";
import { terminals } from "../../lib/terminals";
import { Modal, Check } from "../ui";
import { useT } from "../../lib/i18n";

export default function PromptDialog({ req }: { req: P.Request }) {
  const t = useT();
  const drop = useStore((s) => s.dropPrompt);
  const tab = useStore((s) => s.tabs.find((t) => t.id === req.tabId));
  const [answers, setAnswers] = useState<string[]>([]);
  const [save, setSave] = useState(false);
  useEffect(() => {
    setAnswers((req.fields ?? []).map(() => ""));
    setSave(false);
  }, [req.id]);
  const answer = (ok: boolean) => {
    SystemService.AnswerPrompt({ id: req.id, ok, answers, save });
    drop(req.id);
    // give keyboard focus back to the terminal that asked
    setTimeout(() => {
      const st = useStore.getState();
      if (!st.prompts.length) terminals.get(req.tabId || st.activeId || "")?.focus();
    }, 50);
  };
  const where = tab ? `［${tab.title}］ ` : "";
  // Localized text by prompt kind; the backend's English title/message/labels
  // are the fallback (e.g. kbdint labels come from the server and stay as-is).
  const p = req.params ?? {};
  const title =
    req.kind === "hostkeybad" ? t("prompt.hostKeyBad.title")
    : req.kind === "hostkey" ? t("prompt.hostKey.title")
    : req.kind === "password" ? t("prompt.password.title")
    : req.kind === "passphrase" ? t("prompt.passphrase.title")
    : req.kind === "kbdint" ? t("prompt.kbdint.title")
    : req.kind === "login" ? t("prompt.login.title")
    : req.title;
  const message =
    req.kind === "hostkeybad" && req.params ? t("prompt.hostKeyBad.message", p)
    : req.kind === "hostkey" && req.params ? t("prompt.hostKey.message", p)
    : req.message;
  const label = (i: number, f: P.Field) =>
    req.kind === "password" ? t("prompt.password.field")
    : req.kind === "passphrase" ? t("prompt.passphrase.field")
    : req.kind === "login" ? t(i === 0 ? "prompt.login.user" : "prompt.login.password")
    : f.label;

  if (req.kind === "hostkeybad")
    return (
      <Modal title={<span className="flex items-center gap-2 text-danger"><ShieldAlert size={18} /> {title}</span>} onClose={() => answer(false)} width={560}
        footer={<button className="btn-primary" onClick={() => answer(false)}>{t("prompt.hostKeyBad.ack")}</button>}>
        <div className="whitespace-pre-wrap select-text">{message}</div>
        {req.fingerprint && <div className="mt-2 rounded bg-panel p-2 font-mono text-[13px] select-text">{req.fingerprint}</div>}
      </Modal>
    );

  if (req.kind === "hostkey")
    return (
      <Modal title={<span className="flex items-center gap-2"><ShieldAlert size={18} className="text-warn" /> {where}{title}</span>} onClose={() => answer(false)} width={560} closeOnBackdrop={false}
        footer={
          <>
            <button className="btn" onClick={() => answer(false)}>{t("prompt.hostKey.reject")}</button>
            <button className="btn-primary" autoFocus onClick={() => answer(true)}>{t("prompt.hostKey.trust")}</button>
          </>
        }>
        <div className="whitespace-pre-wrap">{message}</div>
        <div className="mt-2 rounded bg-panel p-2 font-mono text-[13px] select-text">{req.fingerprint}</div>
        <div className="mt-2 text-[13px] text-mute">{t("prompt.hostKey.note")}</div>
      </Modal>
    );

  return (
    <Modal title={<span className="flex items-center gap-2"><KeyRound size={16} /> {where}{title}</span>} onClose={() => answer(false)} width={440} closeOnBackdrop={false}
      footer={
        <>
          <button className="btn" onClick={() => answer(false)}>{t("common.cancel")}</button>
          <button className="btn-primary" onClick={() => answer(true)}>{t("common.ok")}</button>
        </>
      }>
      {req.message && <div className="mb-3 whitespace-pre-wrap text-mute">{req.message}</div>}
      {(req.fields ?? []).map((f, i) => (
        <label key={i} className="mb-2 block">
          <span className="label">{label(i, f)}</span>
          <input
            autoFocus={i === 0}
            className="input"
            type={f.echo ? "text" : "password"}
            value={answers[i] ?? ""}
            onChange={(e) => setAnswers((a) => a.map((x, j) => (j === i ? e.target.value : x)))}
            onKeyDown={(e) => e.key === "Enter" && i === (req.fields?.length ?? 1) - 1 && answer(true)}
          />
        </label>
      ))}
      {req.canSave && (
        <Check checked={save} onChange={setSave}>
          {t(req.kind === "passphrase" ? "prompt.rememberPassphrase" : "prompt.rememberPassword")}
        </Check>
      )}
    </Modal>
  );
}
