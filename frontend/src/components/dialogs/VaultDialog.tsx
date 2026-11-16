import { useState } from "react";
import { SystemService, errMsg } from "../../lib/api";
import { useStore } from "../../lib/store";
import { Modal } from "../ui";

/** Shown on Linux systems without a Secret Service: unlock the encrypted fallback vault. */
export default function VaultDialog() {
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
    <Modal title="解鎖密碼庫" onClose={() => set("vaultOpen", false)} width={440}
      footer={<><button className="btn" onClick={() => set("vaultOpen", false)}>略過</button><button className="btn-primary" onClick={unlock}>解鎖</button></>}>
      <p className="mb-2 text-mute">
        找不到系統鑰匙圈（Secret Service）。已存的密碼改放在以主密碼加密（Argon2id + AES-256-GCM）的檔案裡。第一次輸入的主密碼會用來建立密碼庫。
      </p>
      <input autoFocus type="password" className="input" value={pw} onChange={(e) => setPw(e.target.value)} onKeyDown={(e) => e.key === "Enter" && unlock()} />
      {err && <div className="mt-2 text-danger">{err}</div>}
    </Modal>
  );
}
