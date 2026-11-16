// "servers" strings. Keys are prefixed "servers." ; en must list exactly the same keys.
export const zh = {
  "servers.start": "啟動",
  "servers.stop": "停止",
  "servers.copyUrl": "複製網址",
  "servers.sharedFolder": "分享的資料夾",
  "servers.openFolder": "開啟資料夾",
  "servers.iface": "網卡",
  "servers.port": "連接埠",
  "servers.userPlaceholder": "帳號（空=匿名）",
  "servers.password": "密碼",
  "servers.allowWrite": "允許上傳 / 寫入",
  "servers.note": "預設唯讀、閒置自動關閉。首次啟動時系統防火牆可能會詢問是否允許。",
  "servers.log": "傳輸紀錄",
  "servers.clear": "清除",
} as const;

export const en: Record<keyof typeof zh, string> = {
  "servers.start": "Start",
  "servers.stop": "Stop",
  "servers.copyUrl": "Copy URL",
  "servers.sharedFolder": "Shared folder",
  "servers.openFolder": "Open folder",
  "servers.iface": "Network interface",
  "servers.port": "Port",
  "servers.userPlaceholder": "Username (empty = anonymous)",
  "servers.password": "Password",
  "servers.allowWrite": "Allow uploads / writes",
  "servers.note": "Read-only by default; stops automatically when idle. Your firewall may ask for permission the first time a server starts.",
  "servers.log": "Transfer log",
  "servers.clear": "Clear",
};
