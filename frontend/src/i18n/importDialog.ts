// "importDialog" strings. Keys are prefixed "importDialog." ; en must list exactly the same keys.
export const zh = {
  "importDialog.defaultFolder": "匯入",
  "importDialog.imported": "已匯入 {n} 個 session",
  "importDialog.title": "匯入 Session 設定",
  "importDialog.importN": "匯入 {n} 個 session",
  "importDialog.intro": "選擇其他終端機工具的 session 設定檔（.ini 或匯出的 .mxtsessions）。會匯入 SSH、Telnet、Serial 與本機 Shell；原檔中的密碼是加密的，不會匯入，第一次連線時再輸入並勾選記住即可。",
  "importDialog.source": "來源檔案",
  "importDialog.notSelected": "（尚未選擇）",
  "importDialog.choose": "選擇…",
  "importDialog.targetFolder": "匯入到資料夾",
  "importDialog.summary": "編碼：{encoding}　可匯入 {n}　略過 {skipped}",
} as const;

export const en: Record<keyof typeof zh, string> = {
  "importDialog.defaultFolder": "Imported",
  "importDialog.imported": "Imported {n} sessions",
  "importDialog.title": "Import Session settings",
  "importDialog.importN": "Import {n} sessions",
  "importDialog.intro": "Choose a session settings file from another terminal tool (.ini or an exported .mxtsessions). SSH, Telnet, Serial and local shell sessions are imported. Passwords in the original file are encrypted and won't be imported — enter them on first connect and check \"remember\".",
  "importDialog.source": "Source file",
  "importDialog.notSelected": "(none selected)",
  "importDialog.choose": "Choose…",
  "importDialog.targetFolder": "Import into folder",
  "importDialog.summary": "Encoding: {encoding} · Importable: {n} · Skipped: {skipped}",
};
