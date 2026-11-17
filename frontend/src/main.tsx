import React from "react";
import { createRoot } from "react-dom/client";
import "./index.css";
import App from "./components/App";
import { isMac } from "./lib/api";
import { useStore } from "./lib/store";
import { currentLang } from "./lib/i18n";
import { loadUserFonts } from "./lib/fonts";

// macOS-only style overrides in index.css hang off this class.
if (isMac) document.documentElement.classList.add("mac");

// Keep <html lang> in sync with the UI language (picks zh-TW glyph forms for CJK).
const syncLang = () => (document.documentElement.lang = currentLang());
syncLang();
useStore.subscribe(syncLang);

// Imported fonts must be loaded before terminals open, or xterm measures the
// fallback font's cell size. Don't let a slow or broken font block startup.
const fontsReady = loadUserFonts().catch((e) => console.warn("user fonts:", e));
Promise.race([fontsReady, new Promise((r) => setTimeout(r, 3000))]).then(() =>
  createRoot(document.getElementById("root")!).render(
    <React.StrictMode>
      <App />
    </React.StrictMode>,
  ),
);
