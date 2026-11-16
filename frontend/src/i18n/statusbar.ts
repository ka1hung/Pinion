// Status bar strings. Keys are prefixed "statusbar.".
export const zh = {
  "statusbar.multiExec": "MultiExec：{n} 個分頁",
  "statusbar.connections": "{n} 個連線",
} as const;

export const en: Record<keyof typeof zh, string> = {
  "statusbar.multiExec": "MultiExec: {n} tabs",
  "statusbar.connections": "{n} connected",
};
