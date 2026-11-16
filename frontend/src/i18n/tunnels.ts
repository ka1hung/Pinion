// "tunnels" strings. Keys are prefixed "tunnels." ; en must list exactly the same keys.
export const zh = {
  "tunnels.remoteToLocal": "遠端 {bind} → 本機 {target}",
  "tunnels.empty": "還沒有通道。在 SSH Session 的「通道」分頁新增 Local / Remote / Dynamic (SOCKS) 轉發。",
  "tunnels.start": "啟動",
  "tunnels.stop": "停止",
  "tunnels.conns": "{count} 個連線",
} as const;

export const en: Record<keyof typeof zh, string> = {
  "tunnels.remoteToLocal": "Remote {bind} → local {target}",
  "tunnels.empty": "No tunnels yet. Add Local / Remote / Dynamic (SOCKS) forwarding in an SSH Session's \"Tunnels\" tab.",
  "tunnels.start": "Start",
  "tunnels.stop": "Stop",
  "tunnels.conns": "{count} connection(s)",
};
