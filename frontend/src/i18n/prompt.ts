// Backend prompt dialogs (password, host key, …). Keys are prefixed "prompt.".
export const zh = {
  "prompt.hostKeyBad.title": "警告：主機金鑰已變更",
  "prompt.hostKeyBad.message":
    "{host} 的主機金鑰已變更（現在是 {keyType}）。\n這可能是中間人攻擊，也可能是設備重新安裝過。\n已拒絕連線。如果這個變更是預期的，請到「設定 → 已知主機」刪除舊的金鑰（{path} 第 {line} 行）。",
  "prompt.hostKeyBad.ack": "知道了",
  "prompt.hostKey.title": "未知的主機",
  "prompt.hostKey.message": "無法確認主機 {host} 的真實性。\n{keyType} 金鑰指紋：",
  "prompt.hostKey.reject": "拒絕",
  "prompt.hostKey.trust": "信任並繼續",
  "prompt.hostKey.note": "確認後會寫入 known_hosts；之後若金鑰改變會直接擋下。",
  "prompt.password.title": "密碼",
  "prompt.password.field": "密碼",
  "prompt.passphrase.title": "金鑰密語",
  "prompt.passphrase.field": "密語",
  "prompt.kbdint.title": "身分驗證",
  "prompt.login.title": "登入",
  "prompt.login.user": "使用者名稱",
  "prompt.login.password": "密碼（可留空）",
  "prompt.rememberPassword": "記住密碼（存進系統鑰匙圈）",
  "prompt.rememberPassphrase": "記住密語（存進系統鑰匙圈）",
} as const;

export const en: Record<keyof typeof zh, string> = {
  "prompt.hostKeyBad.title": "WARNING: host key changed",
  "prompt.hostKeyBad.message":
    "The host key for {host} has changed (now {keyType}).\nThis may be a man-in-the-middle attack, or the device was re-installed.\nConnection refused. If the change is expected, remove the old key in Settings → Known hosts (line {line} of {path}).",
  "prompt.hostKeyBad.ack": "Got it",
  "prompt.hostKey.title": "Unknown host",
  "prompt.hostKey.message": "The authenticity of host {host} can't be established.\n{keyType} key fingerprint is:",
  "prompt.hostKey.reject": "Reject",
  "prompt.hostKey.trust": "Trust and continue",
  "prompt.hostKey.note": "Confirming adds it to known_hosts; if the key changes later the connection is blocked.",
  "prompt.password.title": "Password",
  "prompt.password.field": "Password",
  "prompt.passphrase.title": "Key passphrase",
  "prompt.passphrase.field": "Passphrase",
  "prompt.kbdint.title": "Authentication",
  "prompt.login.title": "Login",
  "prompt.login.user": "Username",
  "prompt.login.password": "Password (optional)",
  "prompt.rememberPassword": "Remember password (in the system keychain)",
  "prompt.rememberPassphrase": "Remember passphrase (in the system keychain)",
};
