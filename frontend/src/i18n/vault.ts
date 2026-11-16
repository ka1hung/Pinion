// "vault" strings. Keys are prefixed "vault." ; en must list exactly the same keys.
export const zh = {
  "vault.title": "解鎖密碼庫",
  "vault.skip": "略過",
  "vault.unlock": "解鎖",
  "vault.intro": "找不到系統鑰匙圈（Secret Service）。已存的密碼改放在以主密碼加密（Argon2id + AES-256-GCM）的檔案裡。第一次輸入的主密碼會用來建立密碼庫。",
} as const;

export const en: Record<keyof typeof zh, string> = {
  "vault.title": "Unlock password vault",
  "vault.skip": "Skip",
  "vault.unlock": "Unlock",
  "vault.intro": "No system keychain (Secret Service) was found. Saved passwords are stored in a file encrypted with a master password (Argon2id + AES-256-GCM). The master password you enter the first time is used to create the vault.",
};
