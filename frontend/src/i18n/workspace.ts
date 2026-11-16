// "workspace" strings. Keys are prefixed "workspace." ; en must list exactly the same keys.
export const zh = {
  "workspace.closeOthers": "關閉其他分頁",
  "workspace.reconnect": "重新連線",
  "workspace.duplicate": "再開一個相同連線",
  "workspace.showRight": "顯示在右側窗格",
  "workspace.rightHint": "點這個窗格後再點上方分頁，或在分頁右鍵選「{action}」",
} as const;

export const en: Record<keyof typeof zh, string> = {
  "workspace.closeOthers": "Close other tabs",
  "workspace.reconnect": "Reconnect",
  "workspace.duplicate": "Open another connection",
  "workspace.showRight": "Show in right pane",
  "workspace.rightHint": "Click this pane, then a tab above, or right-click a tab and choose \"{action}\"",
};
