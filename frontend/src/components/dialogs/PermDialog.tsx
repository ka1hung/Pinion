import { useState } from "react";
import { Modal } from "../ui";

interface Props {
  entry: { name: string; path: string; perm: number; isDir: boolean; owner?: string; group?: string };
  onClose: () => void;
  onApply: (octal: string) => void;
}

const WHO = [
  { label: "擁有者 (Owner)", shift: 6 },
  { label: "群組 (Group)", shift: 3 },
  { label: "其他人 (Others)", shift: 0 },
];
const BITS = [
  { label: "讀取 (r)", bit: 4 },
  { label: "寫入 (w)", bit: 2 },
  { label: "執行 (x)", bit: 1 },
];

function toText(mode: number, isDir: boolean) {
  let s = isDir ? "d" : "-";
  for (const w of WHO) for (const [i, c] of ["r", "w", "x"].entries()) s += mode & (BITS[i].bit << w.shift) ? c : "-";
  return s;
}

/** chmod dialog: 3×3 checkboxes kept in sync with an octal field. */
export default function PermDialog({ entry, onClose, onApply }: Props) {
  const [mode, setMode] = useState(entry.perm & 0o7777);
  const [octal, setOctal] = useState((entry.perm & 0o7777).toString(8).padStart(3, "0"));
  const set = (m: number) => {
    setMode(m);
    setOctal(m.toString(8).padStart(3, "0"));
  };
  const valid = /^[0-7]{3,4}$/.test(octal);
  return (
    <Modal
      title="變更權限"
      width={420}
      onClose={onClose}
      footer={
        <>
          <button className="btn" onClick={onClose}>
            取消
          </button>
          <button className="btn-primary" disabled={!valid} onClick={() => onApply(octal)}>
            套用
          </button>
        </>
      }
    >
      <div className="mb-3 truncate font-mono text-[13px] text-mute" title={entry.path}>
        {entry.path}
        {(entry.owner || entry.group) && (
          <span className="ml-2">
            （uid {entry.owner} / gid {entry.group}）
          </span>
        )}
      </div>
      <table className="w-full text-[14px]">
        <thead>
          <tr className="text-mute">
            <th />
            {BITS.map((b) => (
              <th key={b.bit} className="pb-1 font-normal">
                {b.label}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {WHO.map((w) => (
            <tr key={w.shift}>
              <td className="py-1.5 pr-2">{w.label}</td>
              {BITS.map((b) => {
                const bit = b.bit << w.shift;
                return (
                  <td key={b.bit} className="text-center">
                    <input
                      type="checkbox"
                      className="chk h-4 w-4"
                      checked={(mode & bit) !== 0}
                      onChange={(e) => set(e.target.checked ? mode | bit : mode & ~bit)}
                    />
                  </td>
                );
              })}
            </tr>
          ))}
        </tbody>
      </table>
      <div className="mt-4 flex items-center gap-3">
        <label className="flex items-center gap-2">
          <span className="text-mute">八進位</span>
          <input
            className={"input w-20 font-mono " + (valid ? "" : "!border-danger")}
            value={octal}
            maxLength={4}
            onChange={(e) => {
              const v = e.target.value.replace(/[^0-7]/g, "");
              setOctal(v);
              if (/^[0-7]{3,4}$/.test(v)) setMode(parseInt(v, 8));
            }}
            onKeyDown={(e) => e.key === "Enter" && valid && onApply(octal)}
          />
        </label>
        <span className="font-mono text-mute">{toText(mode, entry.isDir)}</span>
      </div>
    </Modal>
  );
}
