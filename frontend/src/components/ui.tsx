import { useEffect, useRef, useState, type ReactNode } from "react";
import { create } from "zustand";
import { X } from "lucide-react";
import { useT } from "../lib/i18n";

export function Modal(props: {
  title: ReactNode;
  onClose: () => void;
  children: ReactNode;
  footer?: ReactNode;
  width?: number;
  closeOnBackdrop?: boolean;
}) {
  useEffect(() => {
    const k = (e: KeyboardEvent) => e.key === "Escape" && props.onClose();
    window.addEventListener("keydown", k);
    return () => window.removeEventListener("keydown", k);
  }, [props.onClose]);
  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40"
      onMouseDown={(e) => e.target === e.currentTarget && props.closeOnBackdrop !== false && props.onClose()}
    >
      <div
        className="flex max-h-[90vh] flex-col rounded-md border border-line bg-base shadow-2xl"
        style={{ width: props.width ?? 520, maxWidth: "95vw" }}
      >
        <div className="flex items-center justify-between border-b border-line px-4 py-2.5">
          <div className="font-semibold">{props.title}</div>
          <button className="icon-btn" onClick={props.onClose}>
            <X size={16} />
          </button>
        </div>
        <div className="min-h-0 flex-1 overflow-auto px-4 py-3">{props.children}</div>
        {props.footer && <div className="flex justify-end gap-2 border-t border-line px-4 py-2.5">{props.footer}</div>}
      </div>
    </div>
  );
}

export function Field(props: { label: ReactNode; children: ReactNode; className?: string; hint?: ReactNode }) {
  return (
    <label className={"block " + (props.className ?? "")}>
      <span className="label">{props.label}</span>
      {props.children}
      {props.hint && <span className="mt-0.5 block text-[12px] text-mute">{props.hint}</span>}
    </label>
  );
}

export function Check(props: { checked: boolean; onChange: (v: boolean) => void; children: ReactNode; title?: string }) {
  return (
    <label className="inline-flex items-center gap-1.5" title={props.title}>
      <input type="checkbox" className="chk" checked={props.checked} onChange={(e) => props.onChange(e.target.checked)} />
      <span>{props.children}</span>
    </label>
  );
}

// ---- context menu ----
export interface MenuItem {
  label?: string;
  icon?: ReactNode;
  onClick?: () => void;
  danger?: boolean;
  disabled?: boolean;
  separator?: boolean;
  shortcut?: string;
}

const useMenu = create<{ x: number; y: number; items: MenuItem[] | null }>(() => ({ x: 0, y: 0, items: null }));

export function openMenu(e: { clientX: number; clientY: number; preventDefault?: () => void }, items: MenuItem[]) {
  e.preventDefault?.();
  useMenu.setState({ x: e.clientX, y: e.clientY, items });
}

export function ContextMenuHost() {
  const { x, y, items } = useMenu();
  const ref = useRef<HTMLDivElement>(null);
  const [pos, setPos] = useState({ x, y });
  useEffect(() => {
    if (!items) return;
    const close = () => useMenu.setState({ items: null });
    const t = setTimeout(() => {
      window.addEventListener("mousedown", close);
      window.addEventListener("blur", close);
    });
    return () => {
      clearTimeout(t);
      window.removeEventListener("mousedown", close);
      window.removeEventListener("blur", close);
    };
  }, [items]);
  useEffect(() => {
    if (!items || !ref.current) return;
    const r = ref.current.getBoundingClientRect();
    setPos({ x: Math.min(x, window.innerWidth - r.width - 4), y: Math.min(y, window.innerHeight - r.height - 4) });
  }, [items, x, y]);
  if (!items) return null;
  return (
    <div
      ref={ref}
      className="fixed z-[60] min-w-[180px] rounded-md border border-line bg-base py-1 shadow-xl"
      style={{ left: pos.x, top: pos.y }}
      onMouseDown={(e) => e.stopPropagation()}
    >
      {items.map((it, i) =>
        it.separator ? (
          <div key={i} className="my-1 border-t border-line" />
        ) : (
          <button
            key={i}
            disabled={it.disabled}
            className={
              "flex w-full items-center gap-2 px-3 py-1 text-left hover:bg-accent hover:text-accentfg disabled:opacity-40 disabled:hover:bg-transparent " +
              (it.danger ? "text-danger" : "")
            }
            onClick={() => {
              useMenu.setState({ items: null });
              it.onClick?.();
            }}
          >
            <span className="w-4 opacity-80">{it.icon}</span>
            <span className="flex-1">{it.label}</span>
            {it.shortcut && <span className="text-[12px] opacity-60">{it.shortcut}</span>}
          </button>
        ),
      )}
    </div>
  );
}

// ---- simple confirm / input dialogs (promise based) ----
type Ask =
  | { kind: "confirm"; title: string; message: ReactNode; danger?: boolean; ok?: string; resolve: (v: boolean) => void }
  | { kind: "input"; title: string; message?: ReactNode; value: string; resolve: (v: string | null) => void; password?: boolean };

const useAsk = create<{ q: Ask | null }>(() => ({ q: null }));

export function confirmDialog(title: string, message: ReactNode, opts?: { danger?: boolean; ok?: string }) {
  return new Promise<boolean>((resolve) => useAsk.setState({ q: { kind: "confirm", title, message, resolve, ...opts } }));
}

export function inputDialog(title: string, value = "", message?: ReactNode, password = false) {
  return new Promise<string | null>((resolve) =>
    useAsk.setState({ q: { kind: "input", title, value, message, resolve, password } }),
  );
}

export function AskHost() {
  const t = useT();
  const q = useAsk((s) => s.q);
  const [val, setVal] = useState("");
  useEffect(() => {
    if (q?.kind === "input") setVal(q.value);
  }, [q]);
  if (!q) return null;
  const done = (v: any) => {
    useAsk.setState({ q: null });
    (q.resolve as (x: any) => void)(v);
  };
  if (q.kind === "confirm")
    return (
      <Modal
        title={q.title}
        onClose={() => done(false)}
        width={440}
        footer={
          <>
            <button className="btn" onClick={() => done(false)}>
              {t("common.cancel")}
            </button>
            <button autoFocus className={q.danger ? "btn-danger" : "btn-primary"} onClick={() => done(true)}>
              {q.ok ?? t("common.ok")}
            </button>
          </>
        }
      >
        <div className="whitespace-pre-wrap">{q.message}</div>
      </Modal>
    );
  return (
    <Modal
      title={q.title}
      onClose={() => done(null)}
      width={440}
      footer={
        <>
          <button className="btn" onClick={() => done(null)}>
            {t("common.cancel")}
          </button>
          <button className="btn-primary" onClick={() => done(val)}>
            {t("common.ok")}
          </button>
        </>
      }
    >
      {q.message && <div className="mb-2 whitespace-pre-wrap">{q.message}</div>}
      <input
        autoFocus
        className="input"
        type={q.password ? "password" : "text"}
        value={val}
        onChange={(e) => setVal(e.target.value)}
        onKeyDown={(e) => e.key === "Enter" && done(val)}
      />
    </Modal>
  );
}

export function fmtBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  const u = ["KB", "MB", "GB", "TB"];
  let i = -1;
  do {
    n /= 1024;
    i++;
  } while (n >= 1024 && i < u.length - 1);
  return `${n.toFixed(n < 10 ? 1 : 0)} ${u[i]}`;
}

export function fmtTime(ms: number): string {
  if (!ms) return "";
  const d = new Date(ms);
  const p = (x: number) => String(x).padStart(2, "0");
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`;
}
