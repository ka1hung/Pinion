import React from "react";
import { createRoot } from "react-dom/client";
import "./index.css";
import App from "./components/App";
import { isMac } from "./lib/api";
import { useStore } from "./lib/store";
import { currentLang } from "./lib/i18n";

// macOS-only style overrides in index.css hang off this class.
if (isMac) document.documentElement.classList.add("mac");

// Keep <html lang> in sync with the UI language (picks zh-TW glyph forms for CJK).
const syncLang = () => (document.documentElement.lang = currentLang());
syncLang();
useStore.subscribe(syncLang);

createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
);
