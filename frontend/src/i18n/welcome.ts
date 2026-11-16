// Welcome screen strings. Keys are prefixed "welcome.".
export const zh = {
  "welcome.tagline": "SSH · Serial · Telnet · SFTP · 通道 · 臨時伺服器",
  "welcome.newSession": "新增 Session",
  "welcome.localTerminal": "本機終端機",
  "welcome.quickConnect": "快速連線",
  "welcome.import": "匯入 Session 設定",
  "welcome.recent": "最近的 Session",
} as const;

export const en: Record<keyof typeof zh, string> = {
  "welcome.tagline": "SSH · Serial · Telnet · SFTP · Tunnels · Ad-hoc servers",
  "welcome.newSession": "New Session",
  "welcome.localTerminal": "Local terminal",
  "welcome.quickConnect": "Quick connect",
  "welcome.import": "Import Session settings",
  "welcome.recent": "Recent Sessions",
};
