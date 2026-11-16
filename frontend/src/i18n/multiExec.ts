// "multiExec" strings. Keys are prefixed "multiExec." ; en must list exactly the same keys.
export const zh = {
  "multiExec.selectAll": "全選",
  "multiExec.selectNone": "全不選",
  "multiExec.mirrorTitle": "在任一勾選分頁打字，同步送到其他勾選分頁",
  "multiExec.mirror": "同步鍵入",
  "multiExec.placeholder": "輸入指令，Enter 送到 {count} 個勾選的分頁",
  "multiExec.send": "送出",
} as const;

export const en: Record<keyof typeof zh, string> = {
  "multiExec.selectAll": "Select all",
  "multiExec.selectNone": "Select none",
  "multiExec.mirrorTitle": "Typing in any checked tab is also sent to the other checked tabs",
  "multiExec.mirror": "Mirror typing",
  "multiExec.placeholder": "Type a command; Enter sends it to {count} checked tab(s)",
  "multiExec.send": "Send",
};
