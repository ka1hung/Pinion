// "quickConnect" strings. Keys are prefixed "quickConnect." ; en must list exactly the same keys.
export const zh = {
  "quickConnect.title": "快速連線",
  "quickConnect.placeholder": "搜尋 session，或輸入 user@host:port / telnet://host / COM3",
  "quickConnect.enterToConnect": "Enter 直接連線到「{q}」",
} as const;

export const en: Record<keyof typeof zh, string> = {
  "quickConnect.title": "Quick connect",
  "quickConnect.placeholder": "Search sessions, or type user@host:port / telnet://host / COM3",
  "quickConnect.enterToConnect": "Press Enter to connect to \"{q}\"",
};
