import { useState } from "react";
import {
  Plus, Terminal, FileText, Columns2, Radio, Download, Settings, PanelLeft, Zap,
} from "lucide-react";
import { useStore } from "../lib/store";
import { openLocal, openAdhoc, parseQuick } from "../lib/actions";
import { openLocalFile } from "../lib/editor";
import { isMac } from "../lib/api";

export default function Toolbar() {
  const st = useStore();
  const [quick, setQuick] = useState("");
  const go = () => {
    const s = parseQuick(quick);
    if (s) {
      openAdhoc(s);
      setQuick("");
    }
  };
  const mod = isMac ? "⌘" : "Ctrl+Shift+";
  return (
    <div
      className="flex h-10 shrink-0 items-center gap-1 border-b border-line bg-side px-2"
      style={{ paddingLeft: isMac ? 78 : 8, ["--wails-draggable" as any]: "drag" }}
    >
      <button className="icon-btn" title={`側邊欄 (${mod}B)`} onClick={() => st.set("sidebarOpen", !st.sidebarOpen)} style={{ ["--wails-draggable" as any]: "no-drag" }}>
        <PanelLeft size={17} />
      </button>
      <div className="mx-1 h-5 border-l border-line" />
      <div className="flex items-center gap-1" style={{ ["--wails-draggable" as any]: "no-drag" }}>
        <button className="btn" title={`新增 Session (${mod}N)`} onClick={() => st.set("sessionEditor", {})}>
          <Plus size={15} /> Session
        </button>
        <button className="btn" title={`本機終端 (${mod}T)`} onClick={openLocal}>
          <Terminal size={15} /> 本機
        </button>
        <button className="btn" title="開啟本機檔案" onClick={() => openLocalFile()}>
          <FileText size={15} /> 開檔
        </button>
        <div className="mx-1 h-5 border-l border-line" />
        <button
          className={"btn " + (st.split.on ? "!border-accent text-accent" : "")}
          title="分割畫面"
          onClick={() => st.setSplit({ on: !st.split.on, focus: "left" })}
        >
          <Columns2 size={15} /> 分割
        </button>
        <button
          className={"btn " + (st.multi.on ? "!border-accent text-accent" : "")}
          title="MultiExec：一次送指令到多個分頁"
          onClick={() => st.setMulti({ on: !st.multi.on })}
        >
          <Radio size={15} /> MultiExec
        </button>
        <button className="btn" title="匯入 Session 設定（.ini / .mxtsessions）" onClick={() => st.set("importOpen", true)}>
          <Download size={15} /> 匯入
        </button>
      </div>
      <div className="flex-1" />
      <div className="flex items-center gap-1" style={{ ["--wails-draggable" as any]: "no-drag" }}>
        <div className="relative">
          <Zap size={14} className="absolute left-2 top-1.5 text-mute" />
          <input
            className="input w-64 pl-7"
            placeholder="快速連線 user@host:port"
            value={quick}
            onChange={(e) => setQuick(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && go()}
            title="ssh://、telnet://、tcp://、udp://、COM3 或 /dev/ttyUSB0"
          />
        </div>
        <button className="icon-btn" title={`設定 (${mod},)`} onClick={() => st.set("settingsOpen", true)}>
          <Settings size={17} />
        </button>
      </div>
    </div>
  );
}
