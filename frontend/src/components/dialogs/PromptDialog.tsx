import { useEffect, useState } from "react";
import { ShieldAlert, KeyRound } from "lucide-react";
import { SystemService, type prompt as P } from "../../lib/api";
import { useStore } from "../../lib/store";
import { terminals } from "../../lib/terminals";
import { Modal, Check } from "../ui";

export default function PromptDialog({ req }: { req: P.Request }) {
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

  if (req.kind === "hostkeybad")
    return (
      <Modal title={<span className="flex items-center gap-2 text-danger"><ShieldAlert size={18} /> {req.title}</span>} onClose={() => answer(false)} width={560}
        footer={<button className="btn-primary" onClick={() => answer(false)}>知道了</button>}>
        <div className="whitespace-pre-wrap select-text">{req.message}</div>
        {req.fingerprint && <div className="mt-2 rounded bg-panel p-2 font-mono text-[13px] select-text">{req.fingerprint}</div>}
      </Modal>
    );

  if (req.kind === "hostkey")
    return (
      <Modal title={<span className="flex items-center gap-2"><ShieldAlert size={18} className="text-warn" /> {where}{req.title}</span>} onClose={() => answer(false)} width={560} closeOnBackdrop={false}
        footer={
          <>
            <button className="btn" onClick={() => answer(false)}>拒絕</button>
            <button className="btn-primary" autoFocus onClick={() => answer(true)}>信任並繼續</button>
          </>
        }>
        <div className="whitespace-pre-wrap">{req.message}</div>
        <div className="mt-2 rounded bg-panel p-2 font-mono text-[13px] select-text">{req.fingerprint}</div>
        <div className="mt-2 text-[13px] text-mute">確認後會寫入 known_hosts；之後若金鑰改變會直接擋下。</div>
      </Modal>
    );

  return (
    <Modal title={<span className="flex items-center gap-2"><KeyRound size={16} /> {where}{req.title}</span>} onClose={() => answer(false)} width={440} closeOnBackdrop={false}
      footer={
        <>
          <button className="btn" onClick={() => answer(false)}>取消</button>
          <button className="btn-primary" onClick={() => answer(true)}>確定</button>
        </>
      }>
      {req.message && <div className="mb-3 whitespace-pre-wrap text-mute">{req.message}</div>}
      {(req.fields ?? []).map((f, i) => (
        <label key={i} className="mb-2 block">
          <span className="label">{f.label}</span>
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
          記住{req.kind === "passphrase" ? "密語" : "密碼"}（存進系統鑰匙圈）
        </Check>
      )}
    </Modal>
  );
}
