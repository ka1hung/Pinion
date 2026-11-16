import { useState } from "react";
import { SystemService, errMsg } from "../../lib/api";
import { useStore } from "../../lib/store";
import { Modal } from "../ui";
import { useT } from "../../lib/i18n";

/** Shown on Linux systems without a Secret Service: unlock the encrypted fallback vault. */
export default function VaultDialog() {
  const t = useT();
  const open = useStore((s) => s.vaultOpen);
  const set = useStore((s) => s.set);
  const [pw, setPw] = useState("");
  const [err, setErr] = useState("");
  if (!open) return null;
  const unlock = async () => {
    try {
      await SystemService.UnlockVault(pw);
      set("vaultOpen", false);
    } catch (e) {
      setErr(errMsg(e));
    }
  };
  return (
    <Modal title={t("vault.title")} onClose={() => set("vaultOpen", false)} width={440}
      footer={<><button className="btn" onClick={() => set("vaultOpen", false)}>{t("vault.skip")}</button><button className="btn-primary" onClick={unlock}>{t("vault.unlock")}</button></>}>
      <p className="mb-2 text-mute">
        {t("vault.intro")}
      </p>
      <input autoFocus type="password" className="input" value={pw} onChange={(e) => setPw(e.target.value)} onKeyDown={(e) => e.key === "Enter" && unlock()} />
      {err && <div className="mt-2 text-danger">{err}</div>}
    </Modal>
  );
}
