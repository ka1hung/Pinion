import { X } from "lucide-react";
import { useStore } from "../lib/store";

export default function Toasts() {
  const toasts = useStore((s) => s.toasts);
  const dismiss = useStore((s) => s.dismissToast);
  return (
    <div className="pointer-events-none fixed bottom-8 right-4 z-[70] flex w-96 flex-col gap-2">
      {toasts.map((t) => (
        <div
          key={t.id}
          className={
            "pointer-events-auto flex items-start gap-2 rounded border bg-base px-3 py-2 shadow-lg " +
            (t.kind === "error" ? "border-danger" : t.kind === "success" ? "border-ok" : "border-line")
          }
        >
          <div className="flex-1 whitespace-pre-wrap break-words select-text">{t.text}</div>
          <button className="icon-btn" onClick={() => dismiss(t.id)}>
            <X size={14} />
          </button>
        </div>
      ))}
    </div>
  );
}
