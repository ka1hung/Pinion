// "transfers" strings. Keys are prefixed "transfers." ; en must list exactly the same keys.
export const zh = {
  "transfers.kind.upload": "上傳",
  "transfers.kind.download": "下載",
  "transfers.kind.copy": "複製",
  "transfers.kind.move": "搬移",
  "transfers.failed": "{kind}失敗：{error}",
} as const;

export const en: Record<keyof typeof zh, string> = {
  "transfers.kind.upload": "Upload",
  "transfers.kind.download": "Download",
  "transfers.kind.copy": "Copy",
  "transfers.kind.move": "Move",
  "transfers.failed": "{kind} failed: {error}",
};
